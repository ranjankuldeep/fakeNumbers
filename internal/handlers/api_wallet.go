package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ranjankuldeep/fakeNumber/internal/database/models"

	"github.com/google/uuid"
	"github.com/labstack/echo/v4"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// Middleware check for maintenance mode
func checkMaintenance(ctx context.Context, serverCol *mongo.Collection) (bool, error) {
	var serverData models.Server
	err := serverCol.FindOne(ctx, bson.M{"server": 0}).Decode(&serverData)
	if err != nil {
		return false, err
	}
	return serverData.Maintenance, nil
}

// Handler to retrieve API key
func ApiKey(c echo.Context) error {
	db := c.Get("db").(*mongo.Database)
	// serverCol := models.InitializeServerCollection(db)
	walletCol := models.InitializeApiWalletuserCollection(db)

	userId := c.QueryParam("userId")
	if userId == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"message": "userId is required"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// isMaintenance, err := checkMaintenance(ctx, serverCol)
	// if err != nil {
	// 	return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Internal server error"})
	// }
	// if isMaintenance {
	// 	return c.JSON(http.StatusForbidden, echo.Map{"error": "Site is under maintenance."})
	// }

	var user models.ApiWalletUser
	objID, _ := primitive.ObjectIDFromHex(userId)
	err := walletCol.FindOne(ctx, bson.M{"userId": objID}).Decode(&user)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "User not found"})
	}
	return c.JSON(http.StatusOK, echo.Map{"api_key": user.APIKey})
}

// Handler to retrieve balance
func BalanceHandler(c echo.Context) error {
	db := c.Get("db").(*mongo.Database)
	// serverCol := models.InitializeServerCollection(db)
	walletCol := models.InitializeApiWalletuserCollection(db)

	apiKey := c.QueryParam("api_key")
	if apiKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid Api Key"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// isMaintenance, err := checkMaintenance(ctx, serverCol)
	// if err != nil {
	// 	return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Internal server error"})
	// }
	// if isMaintenance {
	// 	return c.JSON(http.StatusForbidden, echo.Map{"error": "Site is under maintenance."})
	// }

	var user models.ApiWalletUser
	err := walletCol.FindOne(ctx, bson.M{"api_key": apiKey}).Decode(&user)
	if err != nil {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "User not found"})
	}
	return c.JSON(http.StatusOK, echo.Map{"balance": user.Balance})
}

// Handler to change API key
func ChangeAPIKeyHandler(c echo.Context) error {
	db := c.Get("db").(*mongo.Database)
	serverCol := models.InitializeServerCollection(db)
	walletCol := models.InitializeApiWalletuserCollection(db)

	userId := c.QueryParam("userId")
	if userId == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"message": "UserId is required"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	isMaintenance, err := checkMaintenance(ctx, serverCol)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Internal server error"})
	}
	if isMaintenance {
		return c.JSON(http.StatusForbidden, echo.Map{"error": "Site is under maintenance."})
	}

	newApiKey := uuid.New().String()

	filter := bson.M{"userId": userId}
	update := bson.M{"$set": bson.M{"api_key": newApiKey}}

	_, err = walletCol.UpdateOne(ctx, filter, update)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to update API key"})
	}

	return c.JSON(http.StatusOK, echo.Map{"message": "API key updated successfully", "api_key": newApiKey})
}

// Handler to update UPI QR code
func UpiQRUpdateHandler(c echo.Context) error {
	file := c.FormValue("file")
	if file == "" {
		return c.String(http.StatusBadRequest, "QR code file is required")
	}

	base64Data := file[strings.IndexByte(file, ',')+1:]
	bufferData, err := base64.StdEncoding.DecodeString(base64Data)
	if err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "Invalid file format"})
	}

	filePath := filepath.Join("uploads", "upi-qr-code.png")
	os.MkdirAll(filepath.Dir(filePath), os.ModePerm)

	err = ioutil.WriteFile(filePath, bufferData, 0644)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to save QR code"})
	}

	return c.String(http.StatusOK, "QR code updated successfully")
}

// Handler to create or update API key for recharge type
func CreateOrUpdateAPIKeyHandler(c echo.Context) error {
	db := c.Get("db").(*mongo.Database)
	rechargeCol := models.InitializeRechargeAPICollection(db)

	apiKey := c.FormValue("api_key")
	rechargeType := c.FormValue("recharge_type")
	if rechargeType == "" || apiKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "API key and recharge_type are required"})
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var existingAPI models.RechargeAPI
	err := rechargeCol.FindOne(ctx, bson.M{"recharge_type": rechargeType}).Decode(&existingAPI)
	if err == mongo.ErrNoDocuments {
		// Create new API key
		_, err = rechargeCol.InsertOne(ctx, models.RechargeAPI{
			RechargeType: rechargeType,
			APIKey:       apiKey,
		})
		if err != nil {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to create API key"})
		}
		return c.JSON(http.StatusCreated, echo.Map{"message": "API key created successfully"})
	} else if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Internal server error"})
	}

	// Update existing API key
	_, err = rechargeCol.UpdateOne(ctx, bson.M{"recharge_type": rechargeType}, bson.M{"$set": bson.M{"api_key": apiKey}})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to update API key"})
	}
	return c.JSON(http.StatusOK, echo.Map{"message": "API key updated successfully"})
}

// Handler to retrieve the UPI QR code
func GetUpiQR(c echo.Context) error {
	db := c.Get("db").(*mongo.Database)
	serverCol := db.Collection("servers") // Ensure this matches your actual MongoDB collection name

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Check for maintenance mode
	var serverData struct {
		Maintenance bool `bson:"maintainance"`
	}
	err := serverCol.FindOne(ctx, bson.M{"server": 0}).Decode(&serverData)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.JSON(http.StatusNotFound, echo.Map{"error": "Server data not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Internal server error"})
	}

	if serverData.Maintenance {
		return c.JSON(http.StatusForbidden, echo.Map{"error": "Site is under maintenance."})
	}

	// Define the file path where the QR code is saved
	filePath := filepath.Join("uploads", "upi-qr-code.png")

	// Check if the file exists
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return c.JSON(http.StatusNotFound, echo.Map{"error": "QR code file not found"})
	}

	// Send the file as a response
	return c.File(filePath)
}

// UpdateBalanceHandler handles balance updates for a user
func UpdateBalanceHandler(c echo.Context) error {
	db := c.Get("db").(*mongo.Database)
	walletCol := db.Collection("api_wallet_users")

	// Parse request body
	var requestBody struct {
		UserID     string  `json:"userId"`
		NewBalance float64 `json:"new_balance"`
	}
	if err := c.Bind(&requestBody); err != nil {
		return c.JSON(http.StatusBadRequest, echo.Map{"message": "Invalid request body"})
	}

	// Validate input
	if requestBody.UserID == "" || requestBody.NewBalance == 0 {
		return c.JSON(http.StatusBadRequest, echo.Map{"message": "User ID and new_balance are required"})
	}

	// Create a MongoDB context with a timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Find the user by userId
	var user struct {
		Balance float64 `bson:"balance"`
	}
	err := walletCol.FindOne(ctx, bson.M{"userId": requestBody.UserID}).Decode(&user)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.JSON(http.StatusNotFound, echo.Map{"message": "User not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to fetch user"})
	}

	// Calculate the balance difference
	oldBalance := user.Balance
	balanceDifference := requestBody.NewBalance - oldBalance

	// Save the recharge history if there's a balance change
	if balanceDifference != 0 {
		rechargeHistory := map[string]interface{}{
			"userId":         requestBody.UserID,
			"transaction_id": time.Now().UnixNano(), // Unique transaction ID
			"amount":         fmt.Sprintf("%.2f", balanceDifference),
			"payment_type":   "Admin Added",
			"date_time":      time.Now().Format("01/02/2006T03:04:05 PM"), // Format: MM/DD/YYYYThh:mm:ss A
			"status":         "Received",
		}

		// Prepare request for saving recharge history
		rechargeHistoryURL := fmt.Sprintf("%s/api/save-recharge-history", c.Echo().Server.Addr)
		rechargeHistoryJSON, _ := json.Marshal(rechargeHistory)
		req, err := http.NewRequest("POST", rechargeHistoryURL, bytes.NewBuffer(rechargeHistoryJSON))
		if err != nil {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to create recharge history request"})
		}
		req.Header.Set("Content-Type", "application/json")

		client := &http.Client{Timeout: 5 * time.Second}
		resp, err := client.Do(req)
		if err != nil || resp.StatusCode != http.StatusOK {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to save recharge history"})
		}
		defer resp.Body.Close()
	}

	// Update the user's balance in the database
	update := bson.M{"$set": bson.M{"balance": requestBody.NewBalance}}
	_, err = walletCol.UpdateOne(ctx, bson.M{"userId": requestBody.UserID}, update, options.Update())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Failed to update balance"})
	}

	return c.JSON(http.StatusOK, echo.Map{
		"message": "Balance updated successfully",
		"balance": requestBody.NewBalance,
	})
}

// GetAPIKeyHandler handles fetching an API key based on recharge type
func GetAPIKeyHandler(c echo.Context) error {
	db := c.Get("db").(*mongo.Database)
	rechargeCol := db.Collection("recharge_api") // Ensure this matches your collection name

	// Get the "type" query parameter
	rechargeType := c.QueryParam("type")

	// Validate that the type is provided
	if rechargeType == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"message": "recharge_type is required"})
	}

	// MongoDB context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Query the database for the document with the specified recharge type
	var doc struct {
		APIKey string `bson:"api_key"`
	}
	err := rechargeCol.FindOne(ctx, bson.M{"recharge_type": rechargeType}).Decode(&doc)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return c.JSON(http.StatusNotFound, echo.Map{"message": "API key not found"})
		}
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "Internal server error"})
	}

	// Respond with the API key
	return c.JSON(http.StatusOK, echo.Map{"api_key": doc.APIKey})
}
