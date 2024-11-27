package handlers

import (
	"context"
	"fmt"
	"log"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/ranjankuldeep/fakeNumber/internal/database/models"
	"github.com/ranjankuldeep/fakeNumber/internal/services"
	"github.com/ranjankuldeep/fakeNumber/internal/utils"
	"github.com/ranjankuldeep/fakeNumber/logs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

func GetNumberHandlerApi(c echo.Context) error {
	db := c.Get("db").(*mongo.Database)
	apiKey := c.QueryParam("apikey")
	server := c.QueryParam("server")
	serviceNameWithSpaces := c.QueryParam("servicename")
	serviceName := strings.ReplaceAll(serviceNameWithSpaces, "%", " ")
	ctx := context.TODO()

	if apiKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "empty key"})
	}
	if server == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "empty server number"})
	}
	serverNumber, _ := strconv.Atoi(server)
	if serviceName == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "empty service name"})
	}

	var apiWalletUser models.ApiWalletUser
	apiWalletCollection := models.InitializeApiWalletuserCollection(db)
	err := apiWalletCollection.FindOne(context.TODO(), bson.M{"api_key": apiKey}).Decode(&apiWalletUser)
	if err != nil {
		if err == mongo.ErrEmptySlice {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "invalid api key"})
		}
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal server error"})
	}

	userCollection := models.InitializeUserCollection(db)
	var user models.User
	err = userCollection.FindOne(ctx, bson.M{"_id": apiWalletUser.UserID}).Decode(&user)
	if user.Blocked {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "your account is blocked, contact the admin"})
	}

	var serverInfo models.Server
	serverCollection := models.InitializeServerCollection(db)
	err = serverCollection.FindOne(ctx, bson.M{"server": serverNumber}).Decode(&serverInfo)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	if serverInfo.Maintenance == true {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "under maintenance"})
	}

	serverListollection := models.InitializeServerListCollection(db)
	var serverList models.ServerList
	err = serverListollection.FindOne(ctx, bson.M{
		"name":           serviceName,
		"servers.server": serverNumber,
	}).Decode(&serverList)
	if err != nil {
		logs.Logger.Error("couldn't find server list")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	var serverData models.ServerData
	for _, s := range serverList.Servers {
		if s.Server == serverNumber {
			serverData = models.ServerData{
				Price:  s.Price,
				Code:   s.Code,
				Otp:    s.Otp,
				Server: serverNumber,
			}
		}
	}

	isMultiple := "false"
	apiURLRequest, err := constructApiUrl(db, server, serverInfo.APIKey, serverInfo.Token, serverData, isMultiple)
	if err != nil {
		logs.Logger.Error("Couldn't construcrt api url")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	logs.Logger.Info(fmt.Sprintf("url-%s", apiURLRequest.URL))
	numData, err := ExtractNumber(server, apiURLRequest)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	logs.Logger.Info(fmt.Sprintf("id-%s number-%s", numData.Id, numData.Number))

	price, _ := strconv.ParseFloat(serverData.Price, 64)
	discount, err := FetchDiscount(ctx, db, user.ID.Hex(), serviceName, serverNumber)
	price += discount

	// Check user balance
	if apiWalletUser.Balance < price {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "low balance"})
	}

	newBalance := apiWalletUser.Balance - price
	roundedBalance := math.Round(newBalance*100) / 100
	_, err = apiWalletCollection.UpdateOne(ctx, bson.M{"userId": user.ID}, bson.M{"$set": bson.M{"balance": roundedBalance}})
	if err != nil {
		logs.Logger.Error("FAILED_TO_UPDATE_USER_BALANCE")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	transactionHistoryCollection := models.InitializeTransactionHistoryCollection(db)
	transaction := models.TransactionHistory{
		UserID:        apiWalletUser.UserID.Hex(),
		Service:       serviceName,
		TransactionID: numData.Id,
		Price:         fmt.Sprintf("%.2f", price),
		Server:        server,
		OTP:           []string{},
		ID:            primitive.NewObjectID(),
		Number:        numData.Number,
		Status:        "PENDING",
		DateTime:      time.Now().Format("2006-01-02T15:04:05"),
	}
	_, err = transactionHistoryCollection.InsertOne(ctx, transaction)
	if err != nil {
		logs.Logger.Error("error saving transaction history")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	var expirationTime time.Time
	switch server {
	case "1", "2", "3", "4", "5", "6", "8", "9", "10", "11":
		expirationTime = time.Now().Add(19 * time.Minute)
	case "7":
		expirationTime = time.Now().Add(9 * time.Minute)
	}

	orderCollection := models.InitializeOrderCollection(db)
	order := models.Order{
		ID:             primitive.NewObjectID(),
		UserID:         apiWalletUser.UserID,
		Service:        serviceName,
		Price:          price,
		Server:         serverNumber,
		NumberID:       numData.Id,
		Number:         numData.Number,
		OrderTime:      time.Now(),
		ExpirationTime: expirationTime,
	}
	_, err = orderCollection.InsertOne(ctx, order)
	if err != nil {
		logs.Logger.Error("failed to create order")
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server errror"})
	}

	go func(id, number, userId string, db *mongo.Database, ctx context.Context) {
		defer func() {
			if r := recover(); r != nil {
				logs.Logger.Error("Recovered from panic in OTP handling goroutine:", r)
			}
		}()

		var waitDuration time.Duration
		switch server {
		case "1", "2", "3", "4", "5", "6", "7", "8", "9", "10", "11":
			waitDuration = 3 * time.Minute
		}
		time.Sleep(waitDuration)

		// Fetch server data with maintenance check
		serverData, err := getServerDataWithMaintenanceCheck(ctx, db, server)
		if err != nil {
			logs.Logger.Error(err)
			return
		}

		var transactionData models.TransactionHistory
		transactionCollection := models.InitializeTransactionHistoryCollection(db)

		filter := bson.M{
			"userId": userId,
			"id":     id,
		}
		err = transactionCollection.FindOne(ctx, filter).Decode(&transactionData)
		if err != nil {
			logs.Logger.Error(err)
			return
		}
		otpArrived := false
		if len(transactionData.OTP) != 0 {
			otpArrived = true
		}

		if otpArrived == true {
			logs.Logger.Infof("OTP already arrived for transaction %s, skipping third-party call.", id)
			return
		}

		constructedNumberRequest, err := ConstructNumberUrl(server, serverData.APIKey, serverData.Token, id, number)
		if err != nil {
			logs.Logger.Error(err)
			return
		}

		err = CancelNumberThirdParty(constructedNumberRequest.URL, server, id, db, constructedNumberRequest.Headers)
		if err != nil {
			logs.Logger.Error(err)
			return
		}
	}(numData.Id, numData.Number, apiWalletUser.UserID.Hex(), db, ctx)

	if numData.Id == "" || numData.Number == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "no stock"})
	}
	return c.JSON(http.StatusOK, map[string]string{"status": "ok", "id": numData.Id, "number": numData.Number})
}

func GetOTPHandlerApi(c echo.Context) error {
	db := c.Get("db").(*mongo.Database)
	ctx := context.TODO()
	apiKey := c.QueryParam("apikey")
	server := c.QueryParam("server")
	id := c.QueryParam("id")

	if apiKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "empty key"})
	}
	if server == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "empty server number"})
	}
	if id == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "empty id"})
	}

	var transaction models.TransactionHistory
	transactionCollection := models.InitializeTransactionHistoryCollection(db)
	err := transactionCollection.FindOne(context.TODO(), bson.M{"id": id}).Decode(&transaction)
	if err != nil {
		logs.Logger.Info("sdf")
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal server error"})
	}
	serviceName := transaction.Service

	if transaction.Status == "CANCELLED" {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok", "otp": "number cancelled"})
	}
	if len(transaction.OTP) == 0 && transaction.Status == "PENDING" {
		return c.JSON(http.StatusOK, map[string]string{"status": "ok", "otp": "waiting for otp"})
	}

	var apiWalletUser models.ApiWalletUser
	apiWalletCollection := models.InitializeApiWalletuserCollection(db)
	err = apiWalletCollection.FindOne(context.TODO(), bson.M{"api_key": apiKey}).Decode(&apiWalletUser)
	if err != nil {
		if err == mongo.ErrEmptySlice {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "invalid api key"})
		}
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal server error"})
	}

	userCollection := models.InitializeUserCollection(db)
	var userData models.User
	err = userCollection.FindOne(ctx, bson.M{"_id": apiWalletUser.UserID}).Decode(&userData)
	if userData.Blocked {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "your account is blocked, contact the admin"})
	}

	serverData, err := getServerDataWithMaintenanceCheck(ctx, db, server)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}

	constructedOTPRequest, err := constructOtpUrl(server, serverData.APIKey, serverData.Token, id)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "INVALID_SERVER"})
	}

	validOtpList, err := fetchOTP(server, id, constructedOTPRequest)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	for _, validOtp := range validOtpList {
		transactionCollection := models.InitializeTransactionHistoryCollection(db)
		filter := bson.M{"id": id, "otp": validOtp}
		var existingEntry models.TransactionHistory
		err = transactionCollection.FindOne(ctx, filter).Decode(&existingEntry)
		if err == mongo.ErrNoDocuments {
			formattedDateTime := FormatDateTime()
			update := bson.M{
				"$addToSet": bson.M{"otp": validOtp},
				"$set":      bson.M{"date_time": formattedDateTime},
			}

			filter := bson.M{"id": id}
			_, err = transactionCollection.UpdateOne(ctx, filter, update)
			if err != nil {
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			}

			ipDetails, err := utils.GetIpDetails(c)
			if err != nil {
				logs.Logger.Error(err)
				return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
			}
			formattedIpDetails := removeHTMLTags(ipDetails)
			otpDetail := services.OTPDetails{
				Email:       userData.Email,
				ServiceName: existingEntry.Service,
				Price:       existingEntry.Price,
				Server:      existingEntry.Server,
				Number:      existingEntry.Number,
				OTP:         validOtp,
				Ip:          formattedIpDetails,
			}

			err = services.OtpGetDetails(otpDetail)
			if err != nil {
				logs.Logger.Error(err)
			}

			go func(otp string) {
				err := triggerNextOtp(db, server, serviceName, id)
				if err != nil {
					log.Printf("Error triggering next OTP for ID: %s, OTP: %s - %v", id, otp, err)
				} else {
					log.Printf("Successfully triggered next OTP for ID: %s, OTP: %s", id, otp)
				}
			}(validOtp)
		}
	}
	return c.JSON(http.StatusOK, map[string]interface{}{"status": "ok", "otp": transaction.OTP})
}

func CancelNumberHandlerApi(c echo.Context) error {
	db := c.Get("db").(*mongo.Database)
	ctx := context.TODO()
	apiKey := c.QueryParam("apikey")
	server := c.QueryParam("server")
	id := c.QueryParam("id")

	if apiKey == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "empty key"})
	}
	if server == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "empty server number"})
	}
	if id == "" {
		return c.JSON(http.StatusBadRequest, echo.Map{"error": "empty id"})
	}

	transactionCollection := models.InitializeTransactionHistoryCollection(db)
	var existingOrder models.Order
	orderCollection := models.InitializeOrderCollection(db)
	err := orderCollection.FindOne(ctx, bson.M{"numberId": id}).Decode(&existingOrder)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"errror": "internal server error"})
	}

	var apiWalletUser models.ApiWalletUser
	apiWalletCollection := models.InitializeApiWalletuserCollection(db)
	err = apiWalletCollection.FindOne(context.TODO(), bson.M{"api_key": apiKey}).Decode(&apiWalletUser)
	if err != nil {
		if err == mongo.ErrEmptySlice {
			return c.JSON(http.StatusInternalServerError, echo.Map{"error": "invalid api key"})
		}
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, echo.Map{"error": "internal server error"})
	}

	userCollection := models.InitializeUserCollection(db)
	var userData models.User
	err = userCollection.FindOne(ctx, bson.M{"_id": apiWalletUser.UserID}).Decode(&userData)
	if userData.Blocked {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "your account is blocked, contact the admin"})
	}

	serverData, err := getServerDataWithMaintenanceCheck(ctx, db, server)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": err.Error()})
	}
	if serverData.Maintenance == true {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "under maintenance"})
	}

	timeDifference := time.Now().Sub(existingOrder.OrderTime)
	if timeDifference < 2*time.Minute {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "wait 2 mints before cancel"})
	}

	var transactionData models.TransactionHistory
	filter := bson.M{
		"userId": apiWalletUser.UserID.Hex(),
		"id":     id,
	}
	err = transactionCollection.FindOne(ctx, filter).Decode(&transactionData)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	otpArrived := false
	if len(transactionData.OTP) != 0 {
		otpArrived = true
	}
	if otpArrived == true {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "otp already come"})
	}

	constructedNumberRequest, err := ConstructNumberUrl(server, serverData.APIKey, serverData.Token, id, existingOrder.Number)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "invalid server"})
	}

	err = CancelNumberThirdParty(constructedNumberRequest.URL, server, id, db, constructedNumberRequest.Headers)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	_, err = orderCollection.DeleteOne(ctx, bson.M{"numberId": id})
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	formattedData := FormatDateTime()

	var transaction models.TransactionHistory
	err = transactionCollection.FindOne(ctx, bson.M{"id": id}).Decode(&transaction)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}

	transactionUpdateFilter := bson.M{"id": id}
	transactionpdate := bson.M{
		"$set": bson.M{
			"status":    "CANCELLED",
			"date_time": formattedData,
		},
	}

	_, err = transactionCollection.UpdateOne(ctx, transactionUpdateFilter, transactionpdate)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": err.Error()})
	}
	price, err := strconv.ParseFloat(transaction.Price, 64)
	newBalance := apiWalletUser.Balance + price
	newBalance = math.Round(newBalance*100) / 100

	update := bson.M{
		"$set": bson.M{"balance": newBalance},
	}
	balanceFilter := bson.M{"_id": apiWalletUser.UserID}

	_, err = apiWalletCollection.UpdateOne(ctx, balanceFilter, update)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}

	ipDetails, err := utils.GetIpDetails(c)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "internal server error"})
	}
	services.NumberCancelDetails(userData.Email, transaction.Service, price, server, int64(price), apiWalletUser.Balance, ipDetails)
	return c.JSON(http.StatusOK, map[string]string{"status": "success"})
}