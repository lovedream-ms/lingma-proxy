package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	"lingma-ipc-proxy/internal/lingmaipc"
	"lingma-ipc-proxy/internal/modelmeta"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Discover the active IPC transport.
	opts, err := lingmaipc.ResolveDialOptions(lingmaipc.TransportAuto, "", "")
	if err != nil {
		log.Fatalf("Failed to resolve dial options: %v", err)
	}

	fmt.Printf("Connecting to Lingma/QoderCN IPC...\n")
	fmt.Printf("Transport: %s\n", opts.Transport)
	fmt.Printf("PipePath: %s\n", opts.PipePath)
	fmt.Printf("WebSocketURL: %s\n", opts.WebSocketURL)
	fmt.Println()

	client, err := lingmaipc.Connect(ctx, opts)
	if err != nil {
		log.Fatalf("Failed to connect: %v", err)
	}
	defer client.Close()

	// Initialize the IPC client.
	if err := client.Request(ctx, "initialize", map[string]any{
		"protocolVersion":    1,
		"clientCapabilities": map[string]any{},
		"timestamp":          time.Now().UnixMilli(),
	}, nil); err != nil {
		log.Fatalf("Failed to initialize: %v", err)
	}
	fmt.Println("Initialized successfully")
	fmt.Println()

	// Query the current IPC catalogue.
	var raw any
	if err := client.Request(ctx, "config/queryModels", map[string]any{}, &raw); err != nil {
		log.Fatalf("Failed to query models: %v", err)
	}

	// Print the original response for diagnostics.
	fmt.Println("=== Raw IPC Response ===")
	rawJSON, _ := json.MarshalIndent(raw, "", "  ")
	fmt.Println(string(rawJSON))
	fmt.Println()

	// Share the exact metadata parser used by the HTTP server.
	fmt.Println("=== Extracted Models ===")
	models := modelmeta.Parse(raw)
	for _, m := range models {
		fmt.Printf("ID: %s, Name: %s, Scene: %s, ContextWindow: %d, MaxInputTokens: %d, SupportsText: true, SupportsVision: %t, VisionMetadataProvided: %t\n",
			m.ID, m.Name, m.Scene, m.ContextWindow, m.MaxInputTokens, m.SupportsVision, m.SupportsVisionKnown)
	}
	fmt.Printf("Total enabled models: %d\n", len(models))
}
