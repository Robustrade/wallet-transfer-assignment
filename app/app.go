package app

import (
	"log"
	"wallet-transfer/config"
	"wallet-transfer/internal/database"
	"wallet-transfer/internal/handler"
	"wallet-transfer/internal/repository"
	"wallet-transfer/internal/service"
	"wallet-transfer/urls"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// App holds the application dependencies
type App struct {
	DB              *gorm.DB
	TransferHandler *handler.TransferHandler
	Router          *gin.Engine
}

// InitializeApp sets up all application dependencies
func InitializeApp(cfg config.Config) (*App, error) {
	// Database configuration
	dbConfig := database.Config{
		Host:     cfg.Database.Host,
		Port:     cfg.Database.Port,
		User:     cfg.Database.User,
		Password: cfg.Database.Password,
		DBName:   cfg.Database.DBName,
		SSLMode:  cfg.Database.SSLMode,
	}

	// Connect to database
	db, err := database.NewConnection(dbConfig)
	if err != nil {
		return nil, err
	}

	// Initialize layers
	repo := repository.NewPostgresRepository(db)
	transferService := service.NewTransferService(repo)
	transferHandler := handler.NewTransferHandler(transferService)

	// Setup router
	router := setupRouter(transferHandler)

	return &App{
		DB:              db,
		TransferHandler: transferHandler,
		Router:          router,
	}, nil
}

// setupRouter configures all routes
func setupRouter(transferHandler *handler.TransferHandler) *gin.Engine {
	router := gin.Default()

	// Health check endpoint
	router.GET(urls.HealthEndpoint, func(c *gin.Context) {
		c.JSON(200, gin.H{"status": "healthy"})
	})

	// Transfer endpoints
	router.POST(urls.TransfersEndpoint, transferHandler.CreateTransfer)
	router.GET(urls.TransferByIDEndpoint, transferHandler.GetTransfer)

	return router
}

// Close cleans up application resources
func (app *App) Close() {
	if app.DB != nil {
		sqlDB, err := app.DB.DB()
		if err == nil {
			sqlDB.Close()
		}
	}
}

// Run starts the HTTP server
func (app *App) Run(port string) error {
	log.Printf("Server starting on port %s", port)
	return app.Router.Run(":" + port)
}
