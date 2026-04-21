// ============================================================================
// MBG Smart Logistics — API Client
// Shared API wrapper for all frontend pages to communicate with Go backend
// ============================================================================

const API = {
  BASE_URL: 'http://localhost:8080/api',

  // ── Core HTTP Methods ─────────────────────────────────────────
  async request(method, endpoint, body = null, isFormData = false) {
    const url = `${this.BASE_URL}${endpoint}`;
    const headers = {};
    
    const token = localStorage.getItem('mbg_token');
    if (token) headers['Authorization'] = `Bearer ${token}`;
    if (!isFormData) headers['Content-Type'] = 'application/json';

    const config = { method, headers };
    if (body) {
      config.body = isFormData ? body : JSON.stringify(body);
    }

    try {
      const response = await fetch(url, config);
      const data = await response.json();
      return data;
    } catch (error) {
      console.error(`[API] ${method} ${endpoint} failed:`, error);
      return { status: 0, message: 'Network error: ' + error.message, data: null };
    }
  },

  get(endpoint)         { return this.request('GET', endpoint); },
  post(endpoint, body)  { return this.request('POST', endpoint, body); },
  put(endpoint, body)   { return this.request('PUT', endpoint, body); },
  del(endpoint)         { return this.request('DELETE', endpoint); },
  upload(endpoint, formData) { return this.request('POST', endpoint, formData, true); },

  // ── Pilar 1: OCR & Inventory ─────────────────────────────────
  scanReceipt(imageFile) {
    const form = new FormData();
    form.append('receipt_image', imageFile);
    return this.upload('/ocr/scan-receipt', form);
  },

  confirmOCR(kitchenId, items, imageUrl, rawJson) {
    return this.post('/ocr/confirm', {
      kitchen_id: kitchenId,
      items: items,
      image_url: imageUrl,
      raw_json: rawJson
    });
  },

  getInventory(kitchenId) {
    return this.get(kitchenId ? `/inventory/${kitchenId}` : '/inventory');
  },

  // ── Pilar 2: Production & Shelf-Life ─────────────────────────
  startProduction(dishName, totalPortions, kitchenId, cookedAt, assignments) {
    return this.post('/production/start', {
      dish_name: dishName,
      total_portions: totalPortions,
      kitchen_id: kitchenId,
      cooked_at: cookedAt,
      assignments: assignments
    });
  },

  getActiveProductions() { return this.get('/production/active'); },
  getProduction(id)      { return this.get(`/production/${id}`); },

  // ── Pilar 3 & 4: Routing ─────────────────────────────────────
  planRoutes(productionLogId) {
    return this.post('/routing/plan', { production_log_id: productionLogId });
  },

  getRoutePlans(productionId) { return this.get(`/routing/plans/${productionId}`); },

  completeStop(stopId, deliveredPortions, notes = '') {
    return this.put(`/routing/stops/${stopId}/complete`, {
      delivered_portions: deliveredPortions,
      notes: notes
    });
  },

  // ── Pilar 5: Monitoring ──────────────────────────────────────
  updateLocation(routePlanId, courierId, lat, lng, speed, heading, accuracy) {
    return this.post('/monitoring/location', {
      route_plan_id: routePlanId,
      courier_id: courierId,
      latitude: lat,
      longitude: lng,
      speed_kmh: speed,
      heading: heading,
      accuracy_meters: accuracy
    });
  },

  getTracking(routeId)   { return this.get(`/monitoring/track/${routeId}`); },
  getSchoolETA(schoolId) { return this.get(`/monitoring/eta/${schoolId}`); },

  // ── Agent ────────────────────────────────────────────────────
  analyzeSchedule(routePlanId) {
    return this.post('/agent/analyze-schedule', { route_plan_id: routePlanId });
  },

  recalculateETA(routePlanId, stopId, lat, lng) {
    return this.post('/agent/recalculate-eta', {
      route_plan_id: routePlanId,
      completed_stop_id: stopId,
      courier_lat: lat,
      courier_lng: lng,
      gps_accuracy: 10.0
    });
  },

  // ── Master Data ──────────────────────────────────────────────
  getSchools()  { return this.get('/schools'); },
  getKitchens() { return this.get('/kitchens'); },
  getCouriers() { return this.get('/couriers'); },
  getTrafficStats() { return this.get('/traffic/stats'); },
  healthCheck() { return this.get('/health'); },
};

// ── GPS Helper ──────────────────────────────────────────────────
const GPS = {
  getCurrentPosition() {
    return new Promise((resolve, reject) => {
      if (!navigator.geolocation) {
        reject(new Error('Geolocation not supported'));
        return;
      }
      navigator.geolocation.getCurrentPosition(
        (pos) => resolve({
          lat: pos.coords.latitude,
          lng: pos.coords.longitude,
          accuracy: pos.coords.accuracy,
          speed: pos.coords.speed,
          heading: pos.coords.heading,
          timestamp: pos.timestamp
        }),
        (err) => reject(err),
        { enableHighAccuracy: true, timeout: 10000, maximumAge: 5000 }
      );
    });
  },

  watchPosition(callback) {
    if (!navigator.geolocation) return null;
    return navigator.geolocation.watchPosition(
      (pos) => callback({
        lat: pos.coords.latitude,
        lng: pos.coords.longitude,
        accuracy: pos.coords.accuracy,
        speed: pos.coords.speed,
        heading: pos.coords.heading
      }),
      (err) => console.warn('[GPS] Watch error:', err),
      { enableHighAccuracy: true, maximumAge: 10000 }
    );
  },

  clearWatch(id) {
    if (id) navigator.geolocation.clearWatch(id);
  }
};

// ── Time Helpers ────────────────────────────────────────────────
function formatTime(isoString) {
  if (!isoString) return '--:--';
  return new Date(isoString).toLocaleTimeString('id-ID', { hour: '2-digit', minute: '2-digit' });
}

function formatDateTime(isoString) {
  if (!isoString) return '-';
  return new Date(isoString).toLocaleString('id-ID', {
    day: 'numeric', month: 'short', hour: '2-digit', minute: '2-digit'
  });
}

function minutesFromNow(isoString) {
  if (!isoString) return 0;
  return Math.round((new Date(isoString) - new Date()) / 60000);
}
