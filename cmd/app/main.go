package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/sessions"
	"github.com/joho/godotenv"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/ranjankuldeep/fakeNumber/internal/database"
	"github.com/ranjankuldeep/fakeNumber/internal/lib"
	"github.com/ranjankuldeep/fakeNumber/internal/routes"
	"github.com/ranjankuldeep/fakeNumber/internal/runner"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

var (
	store = sessions.NewCookieStore([]byte("mY FUckingSEcretKey"))
)

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatalf("Error loading .env file: %v", err)
	}
	e := echo.New()
	databaseName := os.Getenv("MONGODB_DATABASE")
	uri := os.Getenv("MONGODB_URI")
	log.Println(uri)

	e.Use(middleware.CORSWithConfig(middleware.CORSConfig{
		AllowOrigins:     []string{"http://localhost:5173", "http://localhost:5174", "https://paidsms.in", "https://makapyar.paidsms.in"},
		AllowMethods:     []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete, http.MethodOptions},
		AllowHeaders:     []string{"Authorization", "Content-Type"},
		AllowCredentials: true,
	}))
	client, err := database.ConnectDB(databaseName, uri)
	if err != nil {
		log.Fatal("Error initializing MongoDB connection:", err)
	}
	db := client.Database(databaseName)
	stats, err := fetchDatabaseStats(db)
	if err != nil {
		log.Printf("Error fetching database stats: %v", err)
	} else {
		log.Printf("Database stats: %v", stats)
	}
	go func() {
		for {
			err := lib.UpdateServerToken(db)
			if err != nil {
				log.Printf("Error during token update: %v", err)
			}
			log.Println("Server token update task completed.")
			time.Sleep(2 * time.Hour)
		}
	}()

	e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			c.Set("db", db)
			return next(c)
		}
	})
	routes.RegisterServiceRoutes(e)
	routes.RegisterGetDataRoutes(e)
	routes.RegisterUserRoutes(e)
	routes.RegisterApiWalletRoutes(e)
	routes.RegisterHistoryRoutes(e)
	routes.RegisterRechargeRoutes(e)
	routes.RegisterUserDiscountRoutes(e)
	routes.RegisterServerRoutes(e)
	routes.RegisterServiceDiscountRoutes(e)
	routes.RegisterServerDiscountRoutes(e)
	routes.RegisterBlockUsersRoutes(e)
	go runner.MonitorOrders(db)
	go func() {
		for {
			runner.CheckAndBlockUsers(db)
			time.Sleep(1 * time.Second)
		}
	}()
	go runner.StartSellingTicker(db)
	e.Logger.Fatal(e.Start(":8000"))
}

func fetchDatabaseStats(db *mongo.Database) (bson.M, error) {
	var result bson.M
	err := db.RunCommand(context.TODO(), bson.D{{Key: "dbStats", Value: 1}}).Decode(&result)
	if err != nil {
		return nil, err
	}
	return result, nil
}
