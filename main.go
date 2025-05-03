// Package main provides a WhatsApp Cloud API to OpenAI real-time voice API integration
// allowing users to have voice conversations with AI assistants through WhatsApp calls.
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strings"
)

// AgentInstruction defines what the AI assistant will say and how it will behave during calls
const AgentInstruction = `
Greet the user with warm welcome.
You are an expert in the field of science. 
You should answer to user's questions in at most 200 words. Not more than that.
Once you finish generating answer, provide user with interseting next steps.
`

// OpenAIAPIBaseURL is the base URL for OpenAI API requests
const OpenAIAPIBaseURL = "https://api.openai.com/v1"

// OpenAIModel specifies which real-time model to use
// gpt-4o-mini-realtime-preview or gpt-4o-realtime-preview
const OpenAIModel = "gpt-4o-mini-realtime-preview"

// Use the Whatsapp API URL
const whatsappAPIURL = "https://wb.omni.tatatelebusiness.com/whatsapp-cloud/calls"

// SDPMessage represents an SDP message in JSON format
type SDPMessage struct {
	SDP  string `json:"sdp"`
	Type string `json:"type"`
}

// WhatsAppCallWebhook represents the incoming webhook structure for WhatsApp calls
type WhatsAppCallWebhook struct {
	Calls struct {
		ID        string `json:"id"`
		From      string `json:"from"`
		To        string `json:"to"`
		Event     string `json:"event"`
		Timestamp string `json:"timestamp"`
		Direction string `json:"direction"`
		Session   struct {
			SDP     string `json:"sdp"`
			SDPType string `json:"sdp_type"`
		} `json:"session"`
	} `json:"calls"`
	BusinessPhoneNumber string `json:"businessPhoneNumber"`
	ID                  string `json:"id"`
	BotID               string `json:"botId"`
	Contacts            []struct {
		Profile struct {
			Name string `json:"name"`
		} `json:"profile"`
		WaID string `json:"wa_id"`
	} `json:"contacts"`
}

// OpenAISessionResponse represents the response from OpenAI's session creation API
type OpenAISessionResponse struct {
	ID           string `json:"id"`
	Object       string `json:"object"`
	ExpiresAt    int    `json:"expires_at"`
	ClientSecret struct {
		Value     string `json:"value"`
		ExpiresAt int    `json:"expires_at"`
	} `json:"client_secret"`
	Model        string   `json:"model"`
	Modalities   []string `json:"modalities"`
	Instructions string   `json:"instructions"`
	Voice        string   `json:"voice"`
}

// WhatsAppCallResponse represents the payload to send to WhatsApp Cloud API
type WhatsAppCallResponse struct {
	MessagingProduct string `json:"messaging_product"`
	CallID           string `json:"call_id"`
	Action           string `json:"action"`
	Connection       struct {
		WebRTC struct {
			SDP string `json:"sdp"`
		} `json:"webrtc"`
	} `json:"connection"`
}

func main() {
	// Set up structured logging with configurable log level
	logLevel := slog.LevelInfo
	if os.Getenv("DEBUG") == "true" {
		logLevel = slog.LevelDebug
	}

	// Choose text or JSON format based on environment
	var handler slog.Handler
	if os.Getenv("LOG_FORMAT") == "json" {
		handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevel,
		})
	} else {
		handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
			Level: logLevel,
		})
	}

	logger := slog.New(handler)
	slog.SetDefault(logger)

	// Get port from environment or use default
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	// Get OpenAI API key from environment
	openAIAPIKey := os.Getenv("OPENAI_API_KEY")
	if openAIAPIKey == "" {
		slog.Error("Missing required environment variable", "variable", "OPENAI_API_KEY")
		os.Exit(1)
	}

	// Get WhatsApp Cloud API token from environment
	whatsappToken := os.Getenv("WHATSAPP_TOKEN")
	if whatsappToken == "" {
		slog.Error("Missing required environment variable", "variable", "WHATSAPP_TOKEN")
		os.Exit(1)
	}

	// Define webhook handler
	http.HandleFunc("/webhook", createWebhookHandler(openAIAPIKey, whatsappToken, whatsappAPIURL))

	// Add a simple health check endpoint
	http.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("OK"))
	})

	// Start HTTP server
	slog.Info("Starting server", "port", port)
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		slog.Error("Server failed", "error", err)
		os.Exit(1)
	}
}

// createWebhookHandler returns an http.HandlerFunc for processing WhatsApp call webhooks
func createWebhookHandler(openAIAPIKey, whatsappToken, whatsappAPIURL string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Only accept POST requests
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Read request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			slog.Error("Failed to read request body", "error", err)
			http.Error(w, "Error reading request body", http.StatusBadRequest)
			return
		}
		defer r.Body.Close()

		slog.Info("Received webhook", "payload_size", len(body))
		if slog.Default().Enabled(r.Context(), slog.LevelDebug) {
			slog.Debug("Webhook payload", "payload", string(body))
		}

		// Parse webhook JSON
		var webhook WhatsAppCallWebhook
		if err := json.Unmarshal(body, &webhook); err != nil {
			slog.Error("Failed to parse webhook JSON", "error", err)
			http.Error(w, "Error parsing webhook JSON", http.StatusBadRequest)
			return
		}

		// Check if this is a connect event
		if webhook.Calls.Event != "connect" {
			slog.Info("Ignoring non-connect event", "event", webhook.Calls.Event)
			w.WriteHeader(http.StatusOK)
			return
		}

		// Prepare SDP multi line string
		sdp := normalizeSdp(webhook.Calls.Session.SDP)

		// Step 1: Create OpenAI ephemeral session
		clientSecret, err := createOpenAISession(openAIAPIKey)
		if err != nil {
			slog.Error("Failed to create OpenAI session", "error", err)
			http.Error(w, "Error creating OpenAI session", http.StatusInternalServerError)
			return
		}

		slog.Info("Created OpenAI session")

		// Step 2: Initiate the WhatsApp call with OpenAI
		sdpAnswer, err := initiateOpenAICall(clientSecret, sdp)
		if err != nil {
			slog.Error("Failed to initiate OpenAI call", "error", err)
			http.Error(w, "Error initiating OpenAI call", http.StatusInternalServerError)
			return
		}

		slog.Debug("Received SDP answer from OpenAI", "sdp_answer_length", len(sdpAnswer))

		// Step 3: Send the SDP answer to WhatsApp Cloud API
		err = sendWhatsAppCallResponse(whatsappToken, webhook.ID, sdpAnswer, whatsappAPIURL)
		if err != nil {
			slog.Error("Failed to send WhatsApp call response", "error", err)
			http.Error(w, "Error sending WhatsApp call response", http.StatusInternalServerError)
			return
		}

		slog.Info("Successfully sent WhatsApp call response", "call_id", webhook.ID)
		w.WriteHeader(http.StatusOK)
	}
}

// createOpenAISession creates an ephemeral session with OpenAI and returns the client secret
func createOpenAISession(apiKey string) (string, error) {
	// Prepare request payload
	payload := map[string]any{
		"instructions": AgentInstruction,
		"model":        OpenAIModel,
		"voice":        "alloy",
		"turn_detection": map[string]any{
			"type": "semantic_vad",
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		slog.Error("Failed to marshal session payload", "error", err)
		return "", fmt.Errorf("error marshaling session payload: %w", err)
	}

	// Create request
	req, err := http.NewRequest(
		http.MethodPost,
		OpenAIAPIBaseURL+"/realtime/sessions",
		bytes.NewBuffer(payloadBytes),
	)
	if err != nil {
		slog.Error("Failed to create session request", "error", err)
		return "", fmt.Errorf("error creating session request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	// Send request
	client := &http.Client{}
	slog.Debug("Sending OpenAI session request")
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Failed to send session request", "error", err)
		return "", fmt.Errorf("error sending session request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Failed to read session response", "error", err)
		return "", fmt.Errorf("error reading session response: %w", err)
	}

	// Check response status
	if resp.StatusCode != http.StatusOK {
		slog.Error("Error creating OpenAI session",
			"status_code", resp.StatusCode,
			"response", string(respBody))
		return "", fmt.Errorf("error creating session: %s", string(respBody))
	}

	// Parse response
	var sessionResp OpenAISessionResponse
	if err := json.Unmarshal(respBody, &sessionResp); err != nil {
		slog.Error("Failed to parse session response", "error", err)
		return "", fmt.Errorf("error parsing session response: %w", err)
	}

	slog.Debug("Successfully created OpenAI session", "session_id", sessionResp.ID)
	// Return client secret
	return sessionResp.ClientSecret.Value, nil
}

// initiateOpenAICall initiates a call with OpenAI using the client secret and SDP
func initiateOpenAICall(clientSecret, sdp string) (string, error) {
	// Create request
	req, err := http.NewRequest(
		http.MethodPost,
		OpenAIAPIBaseURL+"/realtime?model="+OpenAIModel,
		strings.NewReader(sdp),
	)
	if err != nil {
		slog.Error("Failed to create realtime request", "error", err)
		return "", fmt.Errorf("error creating realtime request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/sdp")
	req.Header.Set("Authorization", "Bearer "+clientSecret)

	// Send request
	client := &http.Client{}
	slog.Debug("Sending OpenAI realtime request")
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Failed to send realtime request", "error", err)
		return "", fmt.Errorf("error sending realtime request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Failed to read realtime response", "error", err)
		return "", fmt.Errorf("error reading realtime response: %w", err)
	}

	// Check response status
	if resp.StatusCode != http.StatusCreated {
		slog.Error("Error initiating realtime call",
			"status_code", resp.StatusCode,
			"response", string(respBody))
		return "", fmt.Errorf("error initiating realtime call: %s", string(respBody))
	}

	slog.Debug("Successfully initiated OpenAI call", "response_length", len(respBody))
	// The response body is the SDP answer
	return string(respBody), nil
}

// sendWhatsAppCallResponse sends the SDP answer to the WhatsApp Cloud API
func sendWhatsAppCallResponse(token, callID, sdpAnswer string, apiURL string) error {
	answer, err := generateSDPJSON(sdpAnswer)
	if err != nil {
		slog.Error("Failed to generate SDP JSON", "error", err)
		return fmt.Errorf("SDP answer is empty: %w", err)
	}

	// Create response payload
	response := WhatsAppCallResponse{
		MessagingProduct: "whatsapp",
		CallID:           callID,
		Action:           "accept",
		Connection: struct {
			WebRTC struct {
				SDP string `json:"sdp"`
			} `json:"webrtc"`
		}{
			WebRTC: struct {
				SDP string `json:"sdp"`
			}{
				SDP: answer,
			},
		},
	}

	// Marshal response to JSON
	responseBytes, err := json.Marshal(response)
	if err != nil {
		slog.Error("Failed to marshal WhatsApp response", "error", err)
		return fmt.Errorf("error marshaling WhatsApp response: %w", err)
	}

	// Create request
	req, err := http.NewRequest(
		http.MethodPost,
		apiURL,
		bytes.NewBuffer(responseBytes),
	)
	if err != nil {
		slog.Error("Failed to create WhatsApp request", "error", err)
		return fmt.Errorf("error creating WhatsApp request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", token)

	// Send request
	client := &http.Client{}
	slog.Debug("Sending WhatsApp call response", "call_id", callID)
	resp, err := client.Do(req)
	if err != nil {
		slog.Error("Failed to send WhatsApp request", "error", err, "call_id", callID)
		return fmt.Errorf("error sending WhatsApp request: %w", err)
	}
	defer resp.Body.Close()

	// Read response
	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("Failed to read WhatsApp response", "error", err)
		return fmt.Errorf("error reading WhatsApp response: %w", err)
	}

	// Check response status
	if resp.StatusCode != http.StatusOK {
		slog.Error("Error sending WhatsApp response",
			"status_code", resp.StatusCode,
			"response", string(respBody),
			"call_id", callID)
		return fmt.Errorf("error sending WhatsApp response: %s", string(respBody))
	}

	slog.Debug("Successfully sent WhatsApp call response", "call_id", callID)
	return nil
}

func normalizeSdp(rawSdp string) string {
	// Convert escaped \r\n, \n, or \r to real newline
	reEscaped := regexp.MustCompile(`\\r\\n|\\n|\\r`)
	sdp := reEscaped.ReplaceAllString(rawSdp, "\n")

	// Normalize actual Windows-style \r\n and old-style Mac \r to \n
	sdp = strings.ReplaceAll(sdp, "\r\n", "\n")
	sdp = strings.ReplaceAll(sdp, "\r", "\n")

	// Remove leading whitespace and trim
	sdp = strings.TrimLeft(sdp, " \n\t\r")
	sdp = strings.TrimSpace(sdp)
	sdp = sdp + "\n"

	return sdp
}

// generateSDPJSON takes raw multiline SDP and returns a JSON string with escaped newlines
func generateSDPJSON(multilineSDP string) (string, error) {
	// Normalize line endings to \n
	normalized := strings.ReplaceAll(multilineSDP, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	normalized = strings.TrimSpace(normalized)

	// Create struct with SDP and type
	msg := SDPMessage{
		SDP:  normalized,
		Type: "answer",
	}

	// Marshal to JSON
	jsonBytes, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}

	return string(jsonBytes), nil
}
