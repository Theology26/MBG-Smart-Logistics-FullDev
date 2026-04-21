package osrm

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// ============================================================================
// OSRM Client — Local Routing Engine for Malang
// ============================================================================
// Communicates with OSRM Docker container for:
// - Distance/Duration matrices (Table service)
// - Route geometries (Route service)
// - Nearest road snapping (Nearest service)
// ============================================================================

// Client is the OSRM REST API client.
type Client struct {
	BaseURL    string
	HTTPClient *http.Client
}

// Coordinate represents a geographic point.
type Coordinate struct {
	Lat float64 `json:"lat"`
	Lng float64 `json:"lng"`
}

// NewClient creates a new OSRM client.
func NewClient(baseURL string) *Client {
	return &Client{
		BaseURL: baseURL,
		HTTPClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// ============================================================================
// Table Service — Distance/Duration Matrix
// ============================================================================

// MatrixResponse holds the OSRM table service response.
type MatrixResponse struct {
	Durations [][]float64 `json:"durations"` // seconds
	Distances [][]float64 `json:"distances"` // meters
	Code      string      `json:"code"`      // "Ok" on success
	Message   string      `json:"message"`
}

// GetMatrix returns the full distance and duration matrix for given coordinates.
// Index 0 should be the depot (kitchen), followed by school coordinates.
// Returns durations in seconds and distances in meters.
func (c *Client) GetMatrix(coords []Coordinate) (*MatrixResponse, error) {
	if len(coords) < 2 {
		return nil, fmt.Errorf("need at least 2 coordinates for matrix")
	}

	// Build coordinate string: "lng,lat;lng,lat;..."
	parts := make([]string, len(coords))
	for i, coord := range coords {
		parts[i] = fmt.Sprintf("%f,%f", coord.Lng, coord.Lat)
	}
	coordStr := strings.Join(parts, ";")

	url := fmt.Sprintf("%s/table/v1/driving/%s?annotations=duration,distance",
		c.BaseURL, coordStr)

	log.Printf("🗺️  [OSRM] Requesting %dx%d matrix...", len(coords), len(coords))

	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("OSRM table request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OSRM response: %w", err)
	}

	var result MatrixResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse OSRM response: %w", err)
	}

	if result.Code != "Ok" {
		return nil, fmt.Errorf("OSRM error: %s — %s", result.Code, result.Message)
	}

	log.Printf("🗺️  [OSRM] Matrix received: %dx%d", len(result.Durations), len(result.Distances))
	return &result, nil
}

// ============================================================================
// Route Service — Point-to-Point Routing
// ============================================================================

// RouteResponse holds the OSRM route service response.
type RouteResponse struct {
	Routes []struct {
		Distance float64 `json:"distance"` // meters
		Duration float64 `json:"duration"` // seconds
		Geometry string  `json:"geometry"` // Polyline encoded
		Legs     []struct {
			Distance float64 `json:"distance"`
			Duration float64 `json:"duration"`
			Steps    []struct {
				Distance float64 `json:"distance"`
				Duration float64 `json:"duration"`
				Name     string  `json:"name"`
			} `json:"steps"`
		} `json:"legs"`
	} `json:"routes"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

// GetRoute returns the optimal route between an ordered list of coordinates.
// Used for: getting the actual route geometry and turn-by-turn info.
func (c *Client) GetRoute(coords []Coordinate) (*RouteResponse, error) {
	if len(coords) < 2 {
		return nil, fmt.Errorf("need at least 2 coordinates for route")
	}

	parts := make([]string, len(coords))
	for i, coord := range coords {
		parts[i] = fmt.Sprintf("%f,%f", coord.Lng, coord.Lat)
	}
	coordStr := strings.Join(parts, ";")

	url := fmt.Sprintf("%s/route/v1/driving/%s?overview=full&steps=true&annotations=true",
		c.BaseURL, coordStr)

	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("OSRM route request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OSRM response: %w", err)
	}

	var result RouteResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse OSRM response: %w", err)
	}

	if result.Code != "Ok" {
		return nil, fmt.Errorf("OSRM error: %s — %s", result.Code, result.Message)
	}

	return &result, nil
}

// ============================================================================
// ETA Recalculation — Point-to-Point Duration
// ============================================================================

// GetETABetweenPoints returns the estimated duration between two points.
// Used for dynamic ETA recalculation after each drop-off.
func (c *Client) GetETABetweenPoints(from, to Coordinate) (durationSeconds int, distanceMeters int, err error) {
	coords := []Coordinate{from, to}
	route, err := c.GetRoute(coords)
	if err != nil {
		return 0, 0, err
	}

	if len(route.Routes) == 0 {
		return 0, 0, fmt.Errorf("no route found between points")
	}

	return int(route.Routes[0].Duration), int(route.Routes[0].Distance), nil
}

// ============================================================================
// Nearest Service — Snap to Road
// ============================================================================

// NearestResponse holds the OSRM nearest service response.
type NearestResponse struct {
	Waypoints []struct {
		Location []float64 `json:"location"` // [lng, lat]
		Distance float64   `json:"distance"`  // meters to nearest road
		Name     string    `json:"name"`
	} `json:"waypoints"`
	Code string `json:"code"`
}

// SnapToRoad finds the nearest road point for a GPS coordinate.
// Useful for courier tracking accuracy, especially in dense areas like Dinoyo.
func (c *Client) SnapToRoad(coord Coordinate) (*Coordinate, error) {
	url := fmt.Sprintf("%s/nearest/v1/driving/%f,%f?number=1",
		c.BaseURL, coord.Lng, coord.Lat)

	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return nil, fmt.Errorf("OSRM nearest request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read OSRM response: %w", err)
	}

	var result NearestResponse
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("failed to parse OSRM response: %w", err)
	}

	if result.Code != "Ok" || len(result.Waypoints) == 0 {
		return nil, fmt.Errorf("snap to road failed")
	}

	return &Coordinate{
		Lat: result.Waypoints[0].Location[1],
		Lng: result.Waypoints[0].Location[0],
	}, nil
}

// ============================================================================
// Health Check
// ============================================================================

// IsHealthy checks if the OSRM server is reachable.
func (c *Client) IsHealthy() bool {
	// Use a known Malang coordinate for health check
	url := fmt.Sprintf("%s/nearest/v1/driving/112.6326,-7.9666?number=1", c.BaseURL)
	resp, err := c.HTTPClient.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}
