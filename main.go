package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		return
	}

	cfg := loadConfig()
	client := newOAuthClient(cfg)
	store := fileTokenStore{Path: cfg.AuthFilePath}

	switch os.Args[1] {
	case "login":
		if err := runLogin(client, store); err != nil {
			log.Fatal(err)
		}
	case "refresh":
		if err := runRefresh(client, store); err != nil {
			log.Fatal(err)
		}
	case "token":
		if err := runPrintToken(store); err != nil {
			log.Fatal(err)
		}
	default:
		printUsage()
	}
}

func runLogin(client *OAuthClient, store fileTokenStore) error {
	req, err := client.BuildAuthorizationRequest()
	if err != nil {
		return err
	}

	fmt.Printf("Open this URL in your browser:\n\n%s\n\n", req.URL)
	fmt.Println("Waiting for callback on " + client.config.RedirectURL)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	code, err := resolveAuthorizationCode(
		ctx,
		client.config.RedirectURL,
		req.State,
		req.URL,
		openBrowser,
		listenForCallback,
		func(message string) (string, error) {
			fmt.Println(message)
			var input string
			if _, err := fmt.Scanln(&input); err != nil {
				return "", err
			}
			return input, nil
		},
	)
	if err != nil {
		return err
	}

	token, err := client.ExchangeAuthorizationCode(code, req.Verifier)
	if err != nil {
		return err
	}
	if err := store.Save(token); err != nil {
		return err
	}

	fmt.Printf("Saved token to %s\n", store.Path)
	if token.AccountID != "" {
		fmt.Printf("OpenAI account id: %s\n", token.AccountID)
	}
	fmt.Printf("Expires at: %s\n", token.ExpiresAt.Format(time.RFC3339))
	return nil
}

func runRefresh(client *OAuthClient, store fileTokenStore) error {
	token, err := store.Load()
	if err != nil {
		return err
	}
	if token.RefreshToken == "" {
		return fmt.Errorf("refresh_token is missing in %s", store.Path)
	}

	refreshed, err := client.RefreshAccessToken(token.RefreshToken)
	if err != nil {
		return err
	}
	if err := store.Save(refreshed); err != nil {
		return err
	}

	fmt.Printf("Refreshed token and saved to %s\n", store.Path)
	fmt.Printf("Expires at: %s\n", refreshed.ExpiresAt.Format(time.RFC3339))
	return nil
}

func runPrintToken(store fileTokenStore) error {
	token, err := store.Load()
	if err != nil {
		return err
	}

	fmt.Println(token.AccessToken)
	return nil
}

func printUsage() {
	fmt.Println("Usage:")
	fmt.Println("  go run . login")
	fmt.Println("  go run . refresh")
	fmt.Println("  go run . token")
}
