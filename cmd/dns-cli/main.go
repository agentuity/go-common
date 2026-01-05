package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/agentuity/go-common/dns"
	"github.com/redis/go-redis/v9"
)

func main() {
	if len(os.Args) != 4 {
		fmt.Fprintf(os.Stderr, "Usage: %s <redis-url> <hostname> <target>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Example: %s redis://localhost:6379 example.com target.example.com\n", os.Args[0])
		os.Exit(1)
	}

	redisURL := os.Args[1]
	hostname := os.Args[2]
	target := os.Args[3]

	// Parse Redis URL
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing Redis URL: %v\n", err)
		os.Exit(1)
	}

	// Create Redis client
	client := redis.NewClient(opts)
	defer client.Close()

	// Test connection
	ctx := context.Background()
	_, err = client.Ping(ctx).Result()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error connecting to Redis: %v\n", err)
		os.Exit(1)
	}

	// Create DNS Add action for CNAME record
	action := dns.NewAddAction(hostname, dns.RecordTypeCNAME, target).WithTTL(300)

	// Send the DNS action
	result, err := dns.SendDNSAction(ctx, action, dns.WithRedis(client), dns.WithTimeout(time.Second*15))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error sending DNS action: %v\n", err)
		os.Exit(1)
	}

	if result != nil && len(result.IDs) > 0 {
		fmt.Printf("Successfully added CNAME record for %s -> %s\n", hostname, target)
		fmt.Printf("Record ID: %s\n", result.GetID())
	} else {
		fmt.Printf("DNS action sent successfully, but no record ID returned\n")
	}
}
