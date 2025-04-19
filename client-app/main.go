package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/template/html/v2"
)

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

const (
	// You'll need to update this after registering your client
	clientID      = "cm9nyjeon0000genvs7vzgzsj" // This will be set after registration
	redirectURI   = "http://localhost:5000/callback"
	authEndpoint  = "http://localhost:3000/oauth2/authorize"
	tokenEndpoint = "http://localhost:3000/oauth2/token"
)

func main() {
	app := fiber.New(fiber.Config{
		AppName: "OAuth2 Client",
		Views:   html.New("templates", ".html"),
	})
	app.Use(logger.New())
	app.Use(recover.New())

	// Home page with login button
	app.Get("/", func(c *fiber.Ctx) error {
		if clientID == "" {
			return c.SendString("Please register your client application at http://localhost:3000 and update the clientID constant in main.go")
		}
		return c.Render("login", fiber.Map{})
	})

	// Initiate OAuth2 flow
	app.Get("/login", func(c *fiber.Ctx) error {
		// Generate random state
		state := fmt.Sprintf("%d", rand.Int())

		// Build authorization URL
		authURL := fmt.Sprintf("%s?client_id=%s&redirect_uri=%s&response_type=code&state=%s",
			authEndpoint, clientID, redirectURI, state)

		return c.Redirect(authURL)
	})

	// OAuth2 callback handler
	app.Get("/callback", func(c *fiber.Ctx) error {
		code := c.Query("code")
		if code == "" {
			return c.Status(400).SendString("No code provided")
		}

		// Exchange code for token
		tokenReq := struct {
			GrantType   string `json:"grant_type"`
			Code        string `json:"code"`
			RedirectURI string `json:"redirect_uri"`
			ClientID    string `json:"client_id"`
		}{
			GrantType:   "authorization_code",
			Code:        code,
			RedirectURI: redirectURI,
			ClientID:    clientID,
		}

		jsonData, err := json.Marshal(tokenReq)
		if err != nil {
			return c.Status(500).SendString("Error preparing token request")
		}

		resp, err := http.Post(tokenEndpoint, "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			return c.Status(500).SendString("Error exchanging code for token")
		}
		defer resp.Body.Close()

		if resp.StatusCode != 200 {
			return c.Status(500).SendString("Error response from token endpoint")
		}

		var tokenResponse TokenResponse
		if err := json.NewDecoder(resp.Body).Decode(&tokenResponse); err != nil {
			return c.Status(500).SendString("Error parsing token response")
		}

		// Display the access token
		return c.Render("token", fiber.Map{
			"AccessToken": tokenResponse.AccessToken,
			"TokenType":   tokenResponse.TokenType,
			"ExpiresIn":   tokenResponse.ExpiresIn,
		})
	})

	port := "5000"
	slog.Info("Starting client application on port " + port)
	if err := app.Listen(":" + port); err != nil {
		slog.Error("Error starting server", "error", err.Error())
		panic(err)
	}
}
