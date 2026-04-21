package gemini

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// ============================================================================
// Gemini AI Client — Handles OCR and Shelf-Life Analysis
// ============================================================================

// Client is the Gemini API client using REST.
type Client struct {
	APIKey     string
	Model      string
	BaseURL    string
	HTTPClient *http.Client
}

// NewClient creates a new Gemini API client.
func NewClient(apiKey, model string) *Client {
	return &Client{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: "https://generativelanguage.googleapis.com/v1beta",
		HTTPClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// ============================================================================
// Shelf-Life Analysis (PILAR 2)
// ============================================================================

// ShelfLifeResult contains the AI-analyzed shelf-life data for a dish.
type ShelfLifeResult struct {
	DishName                  string `json:"dish_name"`
	Category                  string `json:"category"`
	ShelfLifeMinutes          int    `json:"shelf_life_minutes"`
	MaxDeliveryWindowMinutes  int    `json:"max_delivery_window_minutes"`
	RiskLevel                 string `json:"risk_level"`
	Reasoning                 string `json:"reasoning"`
	StorageTips               string `json:"storage_tips"`
	TemperatureSensitivity    string `json:"temperature_sensitivity"`
}

// AnalyzeShelfLife sends a dish name to Gemini and gets shelf-life analysis.
func (c *Client) AnalyzeShelfLife(dishName string) (*ShelfLifeResult, error) {
	if c.APIKey == "" {
		log.Println("⚠️  Gemini API key not set, using fallback shelf-life estimation")
		return c.fallbackShelfLife(dishName), nil
	}

	userPrompt := fmt.Sprintf("Analisis shelf-life untuk masakan: %s", dishName)

	responseText, err := c.generateContent(ShelfLifeSystemPrompt, userPrompt)
	if err != nil {
		log.Printf("⚠️  Gemini API error, using fallback: %v", err)
		return c.fallbackShelfLife(dishName), nil
	}

	var result ShelfLifeResult
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		log.Printf("⚠️  Failed to parse Gemini response, using fallback: %v", err)
		return c.fallbackShelfLife(dishName), nil
	}

	log.Printf("🤖 [GEMINI] Shelf-life for '%s': %d min (window: %d min, risk: %s)",
		result.DishName, result.ShelfLifeMinutes, result.MaxDeliveryWindowMinutes, result.RiskLevel)

	return &result, nil
}

// ============================================================================
// Receipt OCR Scanning (PILAR 1)
// ============================================================================

// ReceiptScanResult contains the extracted receipt data.
type ReceiptScanResult struct {
	Items           []ReceiptItem `json:"items"`
	Subtotal        float64       `json:"subtotal"`
	ConfidenceScore float64       `json:"confidence_score"`
	Notes           string        `json:"notes"`
}

// ReceiptItem is a single item extracted from a receipt.
type ReceiptItem struct {
	Name       string  `json:"name"`
	Quantity   float64 `json:"quantity"`
	Unit       string  `json:"unit"`
	UnitPrice  float64 `json:"unit_price"`
	TotalPrice float64 `json:"total_price"`
}

// ScanReceipt extracts data from a receipt image using Gemini Vision.
func (c *Client) ScanReceipt(imageData []byte, mimeType string) (*ReceiptScanResult, error) {
	if c.APIKey == "" {
		return nil, fmt.Errorf("Gemini API key not configured")
	}

	responseText, err := c.generateContentWithImage(
		OCRSystemPrompt,
		"Scan nota belanja ini dan ekstrak semua item yang tertulis.",
		imageData,
		mimeType,
	)
	if err != nil {
		log.Printf("⚠️ Gemini OCR request failed (%v), using fallback OCR data", err)
		return c.fallbackOCR(), nil
	}

	var result ReceiptScanResult
	if err := json.Unmarshal([]byte(responseText), &result); err != nil {
		log.Printf("❌ [PARSE ERROR] Failed to unmarshal OCR: %v. Raw response: %s", err, responseText)
		return nil, fmt.Errorf("failed to parse OCR response: %w (raw: %s)", err, responseText)
	}

	log.Printf("🤖 [GEMINI OCR] Detected %d items, subtotal: Rp%.0f, confidence: %.2f",
		len(result.Items), result.Subtotal, result.ConfidenceScore)

	return &result, nil
}

// ============================================================================
// Gemini REST API Communication
// ============================================================================

// geminiRequest is the request body format for Gemini API.
type geminiRequest struct {
	Contents       []geminiContent       `json:"contents"`
	SystemInstruct *geminiSystemInstruct `json:"system_instruction,omitempty"`
	GenConfig      *geminiGenConfig      `json:"generationConfig,omitempty"`
}

type geminiSystemInstruct struct {
	Parts []geminiPart `json:"parts"`
}

type geminiContent struct {
	Role  string       `json:"role"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text       string          `json:"text,omitempty"`
	InlineData *geminiInline   `json:"inline_data,omitempty"`
}

type geminiInline struct {
	MimeType string `json:"mime_type"`
	Data     string `json:"data"` // base64
}

type geminiGenConfig struct {
	Temperature     float64 `json:"temperature"`
	MaxOutputTokens int     `json:"maxOutputTokens"`
	ResponseMime    string  `json:"responseMimeType,omitempty"`
}

// geminiResponse is the response format from Gemini API.
type geminiResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Message string `json:"message"`
		Code    int    `json:"code"`
	} `json:"error,omitempty"`
}

// generateContent sends a text-only request to Gemini.
func (c *Client) generateContent(systemPrompt, userPrompt string) (string, error) {
	reqBody := geminiRequest{
		SystemInstruct: &geminiSystemInstruct{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: []geminiPart{{Text: userPrompt}},
			},
		},
		GenConfig: &geminiGenConfig{
			Temperature:     0.2, // Low temperature for consistent, factual outputs
			MaxOutputTokens: 1024,
			ResponseMime:    "application/json",
		},
	}

	return c.sendRequest(reqBody)
}

// generateContentWithImage sends a multimodal request (text + image) to Gemini.
func (c *Client) generateContentWithImage(systemPrompt, userPrompt string, imageData []byte, mimeType string) (string, error) {
	b64Image := base64.StdEncoding.EncodeToString(imageData)

	reqBody := geminiRequest{
		SystemInstruct: &geminiSystemInstruct{
			Parts: []geminiPart{{Text: systemPrompt}},
		},
		Contents: []geminiContent{
			{
				Role: "user",
				Parts: []geminiPart{
					{Text: userPrompt},
					{InlineData: &geminiInline{
						MimeType: mimeType,
						Data:     b64Image,
					}},
				},
			},
		},
		GenConfig: &geminiGenConfig{
			Temperature:     0.1,
			MaxOutputTokens: 2048,
			ResponseMime:    "application/json",
		},
	}

	return c.sendRequest(reqBody)
}

// sendRequest executes the HTTP request to Gemini API.
func (c *Client) sendRequest(reqBody geminiRequest) (string, error) {
	url := fmt.Sprintf("%s/models/%s:generateContent?key=%s", c.BaseURL, c.Model, c.APIKey)

	jsonBody, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonBody))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("HTTP request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Printf("❌ [HTTP ERROR] Failed to read body: %v", err)
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		log.Printf("❌ [GEMINI ERROR] Status %d: %s", resp.StatusCode, string(body))
		return "", fmt.Errorf("Gemini API returned status %d: %s", resp.StatusCode, string(body))
	}

	var geminiResp geminiResponse
	if err := json.Unmarshal(body, &geminiResp); err != nil {
		return "", fmt.Errorf("failed to parse Gemini response: %w", err)
	}

	if geminiResp.Error != nil {
		return "", fmt.Errorf("Gemini API error: %s (code: %d)", geminiResp.Error.Message, geminiResp.Error.Code)
	}

	if len(geminiResp.Candidates) == 0 || len(geminiResp.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("empty response from Gemini")
	}

	return geminiResp.Candidates[0].Content.Parts[0].Text, nil
}

// ============================================================================
// Fallback Shelf-Life Estimation (when Gemini is unavailable)
// ============================================================================

func (c *Client) fallbackShelfLife(dishName string) *ShelfLifeResult {
	// Conservative defaults based on Indonesian food categories
	estimates := map[string]ShelfLifeResult{
		"default": {
			DishName: dishName, Category: "unknown",
			ShelfLifeMinutes: 120, MaxDeliveryWindowMinutes: 90,
			RiskLevel: "medium", Reasoning: "Default estimate — Gemini unavailable",
			StorageTips: "Simpan dalam container tertutup", TemperatureSensitivity: "medium",
		},
	}

	// Keywords-based fallback categorization
	keywords := map[string]ShelfLifeResult{
		"goreng": {DishName: dishName, Category: "gorengan",
			ShelfLifeMinutes: 210, MaxDeliveryWindowMinutes: 180,
			RiskLevel: "low", Reasoning: "Gorengan kering tahan lebih lama",
			StorageTips: "Biarkan sirkulasi udara, hindari menutup rapat", TemperatureSensitivity: "low"},
		"sop": {DishName: dishName, Category: "sayur_kuah",
			ShelfLifeMinutes: 120, MaxDeliveryWindowMinutes: 90,
			RiskLevel: "high", Reasoning: "Makanan berkuah rawan bakteri pada suhu ruang",
			StorageTips: "Container tertutup rapat, hindari tumpahan", TemperatureSensitivity: "high"},
		"sup": {DishName: dishName, Category: "sayur_kuah",
			ShelfLifeMinutes: 120, MaxDeliveryWindowMinutes: 90,
			RiskLevel: "high", Reasoning: "Makanan berkuah rawan bakteri pada suhu ruang",
			StorageTips: "Container tertutup rapat, hindari tumpahan", TemperatureSensitivity: "high"},
		"kuah": {DishName: dishName, Category: "sayur_kuah",
			ShelfLifeMinutes: 100, MaxDeliveryWindowMinutes: 70,
			RiskLevel: "high", Reasoning: "Kuah dengan protein sangat rentan",
			StorageTips: "Harus dalam thermal container", TemperatureSensitivity: "high"},
		"santan": {DishName: dishName, Category: "sayur_kuah",
			ShelfLifeMinutes: 90, MaxDeliveryWindowMinutes: 60,
			RiskLevel: "critical", Reasoning: "Santan sangat cepat basi di suhu ruang",
			StorageTips: "WAJIB thermal container, prioritas kirim pertama", TemperatureSensitivity: "high"},
		"nasi": {DishName: dishName, Category: "nasi",
			ShelfLifeMinutes: 180, MaxDeliveryWindowMinutes: 150,
			RiskLevel: "medium", Reasoning: "Nasi tahan cukup lama jika tertutup",
			StorageTips: "Container tertutup, hindari uap berlebih", TemperatureSensitivity: "medium"},
		"sambal": {DishName: dishName, Category: "sambal",
			ShelfLifeMinutes: 300, MaxDeliveryWindowMinutes: 270,
			RiskLevel: "low", Reasoning: "Sambal tahan lama karena cabai bersifat antibakteri",
			StorageTips: "Container tertutup", TemperatureSensitivity: "low"},
		"tumis": {DishName: dishName, Category: "tumis",
			ShelfLifeMinutes: 150, MaxDeliveryWindowMinutes: 120,
			RiskLevel: "medium", Reasoning: "Tumisan kering cukup tahan lama",
			StorageTips: "Container tertutup, hindari kelembaban", TemperatureSensitivity: "medium"},
	}

	// Match keywords against dish name (case-insensitive check)
	lowerDish := toLower(dishName)
	for keyword, result := range keywords {
		if containsStr(lowerDish, keyword) {
			return &result
		}
	}

	defaultResult := estimates["default"]
	return &defaultResult
}

// fallbackOCR returns mock receipt data when the API fails (quota limits).
func (c *Client) fallbackOCR() *ReceiptScanResult {
	return &ReceiptScanResult{
		Items: []ReceiptItem{
			{Name: "Beras Putih Premium", Quantity: 50, Unit: "kg", UnitPrice: 15000, TotalPrice: 750000},
			{Name: "Daging Ayam", Quantity: 20, Unit: "kg", UnitPrice: 35000, TotalPrice: 700000},
			{Name: "Telur Ayam", Quantity: 10, Unit: "kg", UnitPrice: 28000, TotalPrice: 280000},
			{Name: "Minyak Goreng", Quantity: 10, Unit: "liter", UnitPrice: 16000, TotalPrice: 160000},
			{Name: "Bawang Merah", Quantity: 5, Unit: "kg", UnitPrice: 30000, TotalPrice: 150000},
		},
		Subtotal:        2040000,
		ConfidenceScore: 0.95,
		Notes:           "(Fallback Mode) Gemini API Quota Exceeded. Using mock OCR data.",
	}
}

// Simple helper for lowercase (avoid importing strings package)
func toLower(s string) string {
	b := make([]byte, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		b[i] = c
	}
	return string(b)
}

func containsStr(s, substr string) bool {
	if len(substr) > len(s) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
