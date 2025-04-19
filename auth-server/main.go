package main

import (
	"crypto/rand"
	"log/slog"
	"os"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"github.com/gofiber/template/html/v2"
	"github.com/golang-jwt/jwt/v5"
	"github.com/joho/godotenv"
	"github.com/lucsky/cuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

type Client struct {
	ID          string         `gorm:"primaryKey"`
	Name        string         `gorm:"unique" form:"name"`
	Website     string         `json:"website" form:"website"`
	Logo        string         `json:"logo" form:"logo"`
	RedirectURI string         `json:"redirect_uri" form:"redirect_uri" gorm:"unique"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}

type AuthCode struct {
	Code      string    `gorm:"primaryKey"`
	ClientID  string    `gorm:"index"`
	CreatedAt time.Time `json:"created_at"`
}

type AuthRequest struct {
	ClientID     string `json:"client_id" query:"client_id"`
	RedirectURI  string `json:"redirect_uri" query:"redirect_uri"`
	ResponseType string `json:"response_type" query:"response_type"`
	Scope        string `json:"scope" query:"scope"`
	State        string `json:"state" query:"state"`
}

type TokenRequest struct {
	GrantType   string `json:"grant_type" form:"grant_type"`
	Code        string `json:"code" form:"code"`
	RedirectURI string `json:"redirect_uri" form:"redirect_uri"`
	ClientID    string `json:"client_id" form:"client_id"`
}

type TokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
	ExpiresIn   int    `json:"expires_in"`
}

type ConsentData struct {
	ClientName  string
	ClientID    string
	RedirectURI string
	State       string
	Scope       string
}

const (
	jwtSecret = "your-secret-key" // In production, this should be an environment variable
)

func main() {
	slog.Info("Starting the application...")
	slog.Info("Loading environment variables...")
	err := godotenv.Load()

	if err != nil {
		panic("Error loading .env file")
	}

	dbURL := os.Getenv("DB_URL")
	if dbURL == "" {
		panic("DB_URL environment variable not set")
	}

	db, err := gorm.Open(postgres.Open(dbURL), &gorm.Config{})
	if err != nil {
		panic("failed to connect database")
	}

	slog.Info("Connected to the database")
	slog.Info("Running migrations...")

	db.AutoMigrate(&Client{}, &AuthCode{})
	slog.Info("Migrations completed")

	api := fiber.New(fiber.Config{
		AppName: "OAuth2 Server",
		Views:   html.New("templates", ".html"),
	})
	api.Use(logger.New())
	api.Use(recover.New())

	// Client registration endpoints
	api.Get("/", func(c *fiber.Ctx) error {
		return c.Render("register", fiber.Map{})
	})

	api.Post("/register", func(c *fiber.Ctx) error {
		client := new(Client)
		if err := c.BodyParser(client); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid request body",
			})
		}

		clientID, err := cuid.NewCrypto(rand.Reader)
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to generate client ID",
			})
		}
		client.ID = clientID

		if err := db.Create(client).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to register client",
			})
		}

		return c.Render("success", fiber.Map{
			"Name":        client.Name,
			"ClientID":    client.ID,
			"RedirectURL": client.RedirectURI,
		})
	})

	// OAuth2 endpoints
	api.Get("/oauth2/authorize", func(c *fiber.Ctx) error {
		var authRequest AuthRequest
		if err := c.QueryParser(&authRequest); err != nil {
			slog.Error("Error parsing query parameters", "error", err.Error())
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "Invalid request",
			})
		}

		if authRequest.ResponseType != "code" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "unsupported_response_type",
			})
		}

		client := Client{}
		if err := db.Where("id = ?", authRequest.ClientID).First(&client).Error; err != nil {
			slog.Error("Client not found", "error", err.Error())
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Client not found",
			})
		}

		if client.RedirectURI != authRequest.RedirectURI {
			slog.Error("Invalid redirect URI")
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "Invalid redirect URI",
			})
		}

		return c.Render("consent", ConsentData{
			ClientName:  client.Name,
			ClientID:    authRequest.ClientID,
			RedirectURI: authRequest.RedirectURI,
			State:       authRequest.State,
			Scope:       authRequest.Scope,
		})
	})

	api.Get("/oauth2/consent", func(c *fiber.Ctx) error {
		approved := c.Query("approved")
		state := c.Query("state")
		clientID := c.Query("client_id")
		redirectURI := c.Query("redirect_uri")

		if approved != "true" {
			return c.Redirect(redirectURI + "?error=access_denied&state=" + state)
		}

		code, err := cuid.NewCrypto(rand.Reader)
		if err != nil {
			slog.Error("Error generating code", "error", err.Error())
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Internal server error",
			})
		}

		// Store the auth code
		authCode := &AuthCode{
			Code:      code,
			ClientID:  clientID,
			CreatedAt: time.Now(),
		}
		if err := db.Create(authCode).Error; err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "Failed to store authorization code",
			})
		}

		return c.Redirect(redirectURI + "?code=" + code + "&state=" + state)
	})

	api.Post("/oauth2/token", func(c *fiber.Ctx) error {
		var tokenRequest TokenRequest
		if err := c.BodyParser(&tokenRequest); err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_request",
			})
		}

		if tokenRequest.GrantType != "authorization_code" {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "unsupported_grant_type",
			})
		}

		// Verify the authorization code
		var authCode AuthCode
		if err := db.Where("code = ? AND client_id = ?", tokenRequest.Code, tokenRequest.ClientID).First(&authCode).Error; err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "invalid_grant",
			})
		}

		// Delete the used authorization code
		db.Delete(&authCode)

		// Generate JWT token
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
			"client_id": tokenRequest.ClientID,
			"exp":       time.Now().Add(time.Hour * 24).Unix(),
			"iat":       time.Now().Unix(),
		})

		tokenString, err := token.SignedString([]byte(jwtSecret))
		if err != nil {
			return c.Status(fiber.StatusInternalServerError).JSON(fiber.Map{
				"error": "server_error",
			})
		}

		return c.JSON(TokenResponse{
			AccessToken: tokenString,
			TokenType:   "Bearer",
			ExpiresIn:   86400, // 24 hours in seconds
		})
	})

	port := os.Getenv("PORT")
	if port == "" {
		port = "3000"
	}
	slog.Info("Starting server on port " + port)
	if err := api.Listen(":" + port); err != nil {
		slog.Error("Error starting server", "error", err.Error())
		panic(err)
	}
}
