package main

import (
	"context"
	"log"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"

	"github.com/joho/godotenv"

	"beatbot/controller"
	"beatbot/handlers"
	"beatbot/youtube"
)

func main() {
	if err := godotenv.Load(); err != nil {
        log.Printf("Warning: Error loading .env file: %v", err)
    }
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	controller := controller.NewController()
	router := gin.Default()

	router.GET("/youtube/search", func(c *gin.Context) {
		query := c.Query("query")
		videos := youtube.Query(query)

		stream, err := youtube.GetVideoStream(videos[0])
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get video stream"})
			return
		}
		
		c.JSON(http.StatusOK, gin.H{
			"videos": videos,
			"stream": stream,
		})
	})

	router.POST("/discord/webhook", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	router.POST("/discord/interactions", func(c *gin.Context) {
		signature := c.GetHeader("X-Signature-Ed25519")
		timestamp := c.GetHeader("X-Signature-Timestamp")

		var bodyBytes []byte
		bodyBytes, err := c.GetRawData()
		if err != nil {
			log.Printf("Error reading body: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read body"})
			return
		}

		options := handlers.Options{
			EnforceVoiceChannel: os.Getenv("ENFORCE_VOICE_CHANNEL") == "true",
		}

		manager := handlers.NewManager(os.Getenv("DISCORD_APP_ID"), controller, options)

		if !manager.VerifyDiscordRequest(signature, timestamp, bodyBytes) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid request signature"})
			return
		}

		interaction, err := manager.ParseInteraction(bodyBytes)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse interaction"})
			return
		}

		response := manager.HandleInteraction(interaction)
		c.JSON(http.StatusOK, response)
	})

	if os.Getenv("NGROK_DOMAIN") != "" && os.Getenv("NGROK_AUTHTOKEN") != "" {
		listener, err := ngrok.Listen(ctx,
			config.HTTPEndpoint(
				config.WithDomain(os.Getenv("NGROK_DOMAIN")),
			),
			ngrok.WithAuthtokenFromEnv(), // defaults to NGROK_AUTHTOKEN
		)
		if err != nil {
			return err
		}

		log.Println("Ngrok URL:", listener.URL())
		return http.Serve(listener, router)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	log.Printf("Starting server on :%s", port)
	return http.ListenAndServe(":"+port, router)
}