package main

import (
	"context"
	"github.com/gin-gonic/gin"
	"github.com/rasadov/MailManagerApp/internal/config"
	"github.com/rasadov/MailManagerApp/internal/database/postgres"
	"github.com/rasadov/MailManagerApp/internal/database/redis"
	"github.com/rasadov/MailManagerApp/internal/handlers"
	"github.com/rasadov/MailManagerApp/internal/middleware"
	"github.com/rasadov/MailManagerApp/internal/services"
	"html/template"
	"log"
	"time"
)

func main() {
	// Initialize databases
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	db := postgres.GetPostgresDB(config.PostgresUrl)
	client := redis.GetRedisClient(ctx, config.RedisAddr, config.RedisPassword)

	// Initialize services
	rateLimiter := services.NewRateLimiter(client, 3, 15*time.Minute)
	authService := services.NewJWTAuthService(config.JWTSecret, config.JWTIssuer, db)
	emailService := services.NewSMTPEmailService(
		config.SmtpHost,
		config.SmtpPort,
		config.SmtpUsername,
		config.SmtpPassword,
		config.BaseUrl,
	)
	subscriberService := services.NewSubscriberService(db)

	// Ensure admin user is present
	err := authService.EnsureAdminUser(config.AdminUsername, config.AdminPassword)

	if err != nil {
		log.Println("Error ensuring admin user")
	}

	// Initialize handlers
	adminHandler := handlers.NewAdminHandler(rateLimiter, authService, emailService, subscriberService)
	subscriberHandler := handlers.NewSubscriberHandler(subscriberService)
	staticHandler := handlers.NewStaticHandler("./web/static")

	// Setup routes
	r := setupRoutes(adminHandler, subscriberHandler, staticHandler, rateLimiter)

	log.Fatal(r.Run(":8080"))
}

func setupRoutes(
	adminHandler *handlers.AdminHandler,
	subscriberHandler *handlers.SubscriberHandler,
	staticHandler *handlers.StaticHandler,
	rateLimiter services.RateLimiter,
) *gin.Engine {
	r := gin.Default()

	r.SetFuncMap(template.FuncMap{
		"add": func(a, b int) int {
			return a + b
		},
		"sub": func(a, b int) int {
			return a - b
		},
		"iterate": func(start, end int) []int {
			var result []int
			for i := start; i <= end; i++ {
				result = append(result, i)
			}
			return result
		},
	})

	// Load HTML templates
	r.LoadHTMLGlob("web/templates/*")

	r.Static("/static", "./web/static")

	// ========== STATIC ROUTES ==========
	r.GET("/favicon.ico", staticHandler.Favicon)
	r.GET("/", staticHandler.Index)

	// ========== SUBSCRIBER ROUTES ==========
	r.POST("/subscribe", subscriberHandler.Subscribe)
	r.GET("/preferences/:uuid", subscriberHandler.ManagePreferencesPage)
	r.POST("/preferences/:uuid", subscriberHandler.UpdatePreferences)

	// ========== ADMIN ROUTES ==========
	// Admin login (with an obscured path)
	r.GET("/admin/xxxloginyyy", adminHandler.AdminLoginPage)
	r.POST("/admin/xxxloginyyy",
		middleware.RateLimitMiddleware(rateLimiter),
		adminHandler.LoginPost,
	)

	// Protected admin routes
	admin := r.Group("/admin")
	admin.Use(middleware.AuthRequired())
	{
		admin.GET("", adminHandler.AdminDashboard)
		admin.POST("/send-email", adminHandler.SendEmail)
	}

	return r
}
