package main

import (
	"context"
	"io"

	"net/http"
	"os"

	"github.com/gin-gonic/gin"
	"golang.ngrok.com/ngrok"
	"golang.ngrok.com/ngrok/config"

	"github.com/joho/godotenv"

	nested "github.com/antonfisher/nested-logrus-formatter"
	log "github.com/sirupsen/logrus"

	appConfig "beatbot/config"
	"beatbot/controller"
	"beatbot/discord"
	"beatbot/gemini"
	"beatbot/handlers"
	"beatbot/youtube"
)

func main() {
	log.SetFormatter(&nested.Formatter{
		HideKeys:     true,
		TrimMessages: true,
	})
	log.SetOutput(os.Stdout)
	log.SetLevel(log.DebugLevel)

	if os.Getenv("RELEASE") == "false" || os.Getenv("RELEASE") == "" {
		if err := godotenv.Load(".env.dev"); err != nil {
			log.Fatalf("Warning: Error loading .env file: %v", err)
		}
	}

	appConfig.NewConfig()
	if err := run(context.Background()); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context) error {
	controller, err := controller.NewController()
	if err != nil {
		log.Fatalf("Error creating controller: %v", err)
		return err
	}
	router := gin.Default()

	if os.Getenv("RELEASE") == "false" || os.Getenv("RELEASE") == "" {
		router.GET("/youtube/search", func(c *gin.Context) {
			query := c.Query("query")
			videos := youtube.Query(query)

			stream, err := youtube.GetVideoStream(videos[0])
			if err != nil {
				c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to get video stream"})
				return
			}

			c.JSON(http.StatusOK, gin.H{
				"stream": stream.StreamURL,
			})
		})

		router.POST("/gemini/rude", func(c *gin.Context) {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read body"})
				return
			}
			prompt := string(bodyBytes)
			response := gemini.GenerateRudeResponse(prompt)
			c.JSON(http.StatusOK, gin.H{
				"response": response,
			})
		})

		router.POST("/gemini/helpful", func(c *gin.Context) {
			bodyBytes, err := io.ReadAll(c.Request.Body)
			if err != nil {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read body"})
				return
			}
			prompt := string(bodyBytes)
			response := gemini.GenerateHelpfulResponse(prompt)
			c.JSON(http.StatusOK, gin.H{
				"response": response,
			})
		})
	}

	router.POST("/discord/webhook", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"message": "pong",
		})
	})

	router.GET("/discord/:guildId/members/:userId", func(c *gin.Context) {
		guildId := c.Param("guildId")
		userId := c.Param("userId")
		discord.GetMemberVoiceState(&userId, &guildId)
		c.JSON(http.StatusOK, gin.H{
			"ok": true,
		})
	})

	router.POST("/discord/interactions", func(c *gin.Context) {
		signature := c.GetHeader("X-Signature-Ed25519")
		timestamp := c.GetHeader("X-Signature-Timestamp")

		var bodyBytes []byte
		bodyBytes, err := c.GetRawData()
		if err != nil {
			log.Errorf("Error reading body: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to read body"})
			return
		}

		manager := handlers.NewManager(os.Getenv("DISCORD_APP_ID"), controller)

		if !manager.VerifyDiscordRequest(signature, timestamp, bodyBytes) {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid request signature"})
			return
		}

		interaction, err := manager.ParseInteraction(bodyBytes)

		// for registering the application, we need to respond with a pong
		if interaction.Type == 1 {
			c.JSON(http.StatusOK, gin.H{
				"type": 1,
			})
			return
		}

		log.Tracef("parsed interaction: %v", interaction)
		if err != nil {
			log.Errorf("Error parsing interaction: %v", err)
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to parse interaction"})
			return
		}

		response := manager.HandleInteraction(interaction)
		c.JSON(http.StatusOK, response)
	})

	if appConfig.Config.NGrok.IsEnabled() {
		log.Info("using ngrok")
		listener, err := ngrok.Listen(ctx,
			config.HTTPEndpoint(
				config.WithDomain(appConfig.Config.NGrok.Domain),
			),
			ngrok.WithAuthtokenFromEnv(), // defaults to NGROK_AUTHTOKEN
		)
		if err != nil {
			return err
		}

		log.Println("Ngrok URL:", listener.URL())
		return http.Serve(listener, router)
	}

	port := appConfig.Config.Options.Port
	if port == "" {
		port = "8080"
	}

	log.Infof("Starting server on :%s", port)
	return http.ListenAndServe(":"+port, router)
}
