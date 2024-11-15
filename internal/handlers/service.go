package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/ranjankuldeep/fakeNumber/internal/database/models"
	"github.com/ranjankuldeep/fakeNumber/internal/lib"
	"github.com/ranjankuldeep/fakeNumber/logs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type ApiRequest struct {
	URL     string
	Headers map[string]string
}

// ResponseData struct to hold parsed data
type ResponseData struct {
	ID     string
	Number string
}

func HandleGetNumberRequest(c echo.Context) error {
	ctx := context.TODO()
	db := c.Get("db").(*mongo.Database)

	// Get query parameters
	serviceCode := c.QueryParam("servicecode")
	apiKey := c.QueryParam("api_key")
	server := c.QueryParam("server")
	serverNumber, _ := strconv.Atoi(server)

	if serviceCode == "" || apiKey == "" || server == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Service code, API key, and Server are required."})
	}

	// Fetch service details
	serverListCollection := models.InitializeServerListCollection(db)
	var serverCode models.ServerList
	err := serverListCollection.FindOne(ctx, bson.M{"service_code": serviceCode}).Decode(&serverCode)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Service not found."})
	}
	serviceName := serverCode.Name

	// Fetch apiWalletUser details for calculating balance
	apiWalletUserCollection := models.InitializeApiWalletuserCollection(db)
	var apiWalletUser models.ApiWalletUser
	err = apiWalletUserCollection.FindOne(ctx, bson.M{"api_key": apiKey}).Decode(&apiWalletUser)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid API key."})
	}

	// Fetch user details and return if user is blocked
	userCollection := models.InitializeUserCollection(db)
	var user models.User
	err = userCollection.FindOne(ctx, bson.M{"_id": apiWalletUser.UserID}).Decode(&user)
	// Check if the user is blocked
	if user.Blocked {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Your account is blocked, contact the Admin."})
	}

	//// Fetch server maintenance data
	// TODO: ALSO HADNLE THE MAITAINENCE
	serverCollection := models.InitializeServerCollection(db)
	var serverInfo models.Server
	err = serverCollection.FindOne(ctx, bson.M{"server": server}).Decode(&serverInfo)
	if err != nil {
		return c.JSON(http.StatusNotFound, map[string]string{"error": "Server not found."})
	}
	server_api_key := serverInfo.APIKey

	serverListollection := models.InitializeServerListCollection(db)

	// Find the server list for the specified server name and server number
	var serverList models.ServerList
	err = serverListollection.FindOne(ctx, bson.M{
		"name":           serviceName,
		"servers.server": server,
	}).Decode(&serverList)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Couldn't find serverlist"})
	}

	// Find the specific server data
	var serviceData models.ServerData
	for _, s := range serverList.Servers {
		if s.Server == serverNumber {
			serviceData = models.ServerData{
				Price:  s.Price,
				Code:   s.Code,
				Otp:    s.Otp,
				Server: serverNumber,
			}
		}
	}

	// fetch id and numbers
	apiURL, err := constructApiUrl(server, server_api_key, serviceData)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Couldn't construcrt api url"})
	}
	var number string
	var id string

	switch apiURL.(type) {
	case string:
		responseData, err := fetchNumber(server, apiURL.(string), map[string]string{})
		if err != nil {
			logs.Logger.Error(err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Couldn't find serverlist"})
		}
		number = responseData.Number
		id = responseData.ID

	case ApiRequest:
		responseData, err := fetchNumber(server, apiURL.(ApiRequest).URL, apiURL.(ApiRequest).Headers)
		if err != nil {
			logs.Logger.Error(err)
			return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Couldn't find serverlist"})
		}

		number = responseData.Number
		id = responseData.ID
	}

	// update the price with the discount
	price, _ := strconv.ParseFloat(serviceData.Price, 64)
	discount, err := FetchDiscount(ctx, db, user.ID.Hex(), serviceName, serverNumber)
	price += discount

	// Check user balance
	if apiWalletUser.Balance < price {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Insufficient balance."})
	}

	// Deduct balance and save to DB
	newBalance := apiWalletUser.Balance - price
	_, err = apiWalletUserCollection.UpdateOne(ctx, bson.M{"_id": user.ID}, bson.M{"$set": bson.M{"balance": newBalance}})
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to update user balance."})
	}

	// Save transaction history
	transactionHistoryCollection := models.InitializeTransactionHistoryCollection(db)
	transaction := models.TransactionHistory{
		UserID:   apiWalletUser.UserID.Hex(),
		Service:  serviceName,
		Price:    fmt.Sprintf("%.2f", price),
		Server:   server,
		ID:       primitive.NewObjectID(),
		Number:   number,
		Status:   "FINISHED",
		DateTime: time.Now().Format("2006-01-02T15:04:05"),
	}
	_, err = transactionHistoryCollection.InsertOne(ctx, transaction)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save transaction history."})
	}

	// Save order
	orderCollection := models.InitializeOrderCollection(db)
	order := models.Order{
		ID:             primitive.NewObjectID(),
		UserID:         apiWalletUser.UserID,
		Service:        serviceName,
		Price:          price,
		Server:         serverNumber,
		NumberID:       id,
		Number:         number,
		OrderTime:      time.Now(),
		ExpirationTime: time.Now().Add(20 * time.Minute),
	}
	_, err = orderCollection.InsertOne(ctx, order)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to save order."})
	}
	return c.JSON(http.StatusOK, map[string]string{"number": number, "id": id})
}

// Helper Functions
func FetchDiscount(ctx context.Context, db *mongo.Database, userId, sname string, server int) (float64, error) {
	totalDiscount := 0.0

	// User-specific discount
	userDiscountCollection := models.InitializeUserDiscountCollection(db)
	var userDiscount models.UserDiscount
	err := userDiscountCollection.FindOne(ctx, bson.M{"userId": userId, "service": sname, "server": server}).Decode(&userDiscount)
	if err != nil && err != mongo.ErrNoDocuments {
		return 0, err
	}
	if err == nil {
		totalDiscount += round(userDiscount.Discount, 2)
	}

	// Service discount
	serviceDiscountCollection := models.InitializeServiceDiscountCollection(db)
	var serviceDiscount models.ServiceDiscount
	err = serviceDiscountCollection.FindOne(ctx, bson.M{"service": sname, "server": server}).Decode(&serviceDiscount)
	if err != nil && err != mongo.ErrNoDocuments {
		return 0, err
	}
	if err == nil {
		totalDiscount += round(serviceDiscount.Discount, 2)
	}

	// Server discount
	serverDiscountCollection := models.InitializeServerDiscountCollection(db)
	var serverDiscount models.ServerDiscount
	err = serverDiscountCollection.FindOne(ctx, bson.M{"server": server}).Decode(&serverDiscount)
	if err != nil && err != mongo.ErrNoDocuments {
		return 0, err
	}
	if err == nil {
		totalDiscount += round(serverDiscount.Discount, 2)
	}

	// Return the total discount rounded to 2 decimal places
	return round(totalDiscount, 2), nil
}

// Helper function to round to 2 decimal places
func round(val float64, precision int) float64 {
	format := fmt.Sprintf("%%.%df", precision)
	valStr := fmt.Sprintf(format, val)
	result, _ := strconv.ParseFloat(valStr, 64)
	return result
}

// Construct API URL function
func constructApiUrl(server, apiKeyServer string, data models.ServerData) (interface{}, error) {
	switch server {

	case "1":
		return fmt.Sprintf(
			"https://fastsms.su/stubs/handler_api.php?api_key=%s&action=getNumber&service=%s&country=22",
			apiKeyServer, data.Code,
		), nil

	case "2":
		return ApiRequest{
			URL: fmt.Sprintf(
				"https://5sim.net/v1/user/buy/activation/india/virtual21/%s",
				data.Code,
			),
			Headers: map[string]string{
				"Authorization": fmt.Sprintf("Bearer %s", apiKeyServer),
				"Accept":        "application/json",
			},
		}, nil

	case "3":
		return fmt.Sprintf(
			"https://smshub.org/stubs/handler_api.php?api_key=%s&action=getNumber&service=%s&operator=any&country=22&maxPrice=%s",
			apiKeyServer, data.Code, data.Price,
		), nil

	case "4":
		return fmt.Sprintf(
			"https://api.tiger-sms.com/stubs/handler_api.php?api_key=%s&action=getNumber&service=%s&country=22",
			apiKeyServer, data.Code,
		), nil

	case "5":
		return fmt.Sprintf(
			"https://api.grizzlysms.com/stubs/handler_api.php?api_key=%s&action=getNumber&service=%s&country=22",
			apiKeyServer, data.Code,
		), nil

	case "6":
		return fmt.Sprintf(
			"https://tempnum.org/stubs/handler_api.php?api_key=%s&action=getNumber&service=%s&country=22",
			apiKeyServer, data.Code,
		), nil

	case "7":
		return fmt.Sprintf(
			"https://api2.sms-man.com/control/get-number?token=%s&application_id=%s&country_id=14&hasMultipleSms=false",
			apiKeyServer, data.Code,
		), nil

	case "8":
		return fmt.Sprintf(
			"https://api2.sms-man.com/control/get-number?token=%s&application_id=%s&country_id=14&hasMultipleSms=true",
			apiKeyServer, data.Code,
		), nil

	case "9":
		return fmt.Sprintf(
			"http://www.phantomunion.com:10023/pickCode-api/push/buyCandy?token=%s&businessCode=%s&quantity=1&country=IN&effectiveTime=10",
			apiKeyServer, data.Code,
		), nil

	default:
		return "", errors.New("invalid server value")
	}
}

// Helper function to handle response data
func handleResponseData(server string, responseData string) (*ResponseData, error) {
	switch server {
	case "1", "3", "4", "5", "6":
		parts := strings.Split(responseData, ":")
		if len(parts) < 3 {
			return nil, errors.New("invalid response format")
		}
		return &ResponseData{
			ID:     parts[1],
			Number: strings.TrimPrefix(parts[2], "91"),
		}, nil

	case "2", "7", "8":
		var jsonResponse map[string]interface{}
		if err := json.Unmarshal([]byte(responseData), &jsonResponse); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		id, ok := jsonResponse["id"].(string)
		if !ok {
			id, _ = jsonResponse["request_id"].(string)
		}
		number, ok := jsonResponse["phone"].(string)
		if !ok {
			number, _ = jsonResponse["number"].(string)
		}
		if id == "" || number == "" {
			return nil, errors.New("missing fields in JSON response")
		}
		return &ResponseData{
			ID:     id,
			Number: strings.TrimPrefix(strings.Replace(number, "+91", "", 1), "91"),
		}, nil

	case "9":
		var jsonResponse map[string]interface{}
		if err := json.Unmarshal([]byte(responseData), &jsonResponse); err != nil {
			return nil, fmt.Errorf("failed to parse JSON: %w", err)
		}
		phoneData, ok := jsonResponse["data"].(map[string]interface{})
		if !ok {
			return nil, errors.New("missing 'data' field in response")
		}
		phoneNumbers, ok := phoneData["phoneNumber"].([]interface{})
		if !ok || len(phoneNumbers) == 0 {
			return nil, errors.New("no phone numbers available")
		}
		firstPhone := phoneNumbers[0].(map[string]interface{})
		id, _ := firstPhone["serialNumber"].(string)
		number, _ := firstPhone["number"].(string)
		return &ResponseData{
			ID:     id,
			Number: strings.TrimPrefix(number, "+91"),
		}, nil

	default:
		return nil, errors.New("no numbers available. Please try different server")
	}
}

// Function to handle the retry logic
func fetchNumber(server string, apiUrl string, headers map[string]string) (*ResponseData, error) {
	client := &http.Client{}
	var retry = true
	var responseData string
	var response *http.Response
	var err error

	for attempt := 0; attempt < 2 && retry; attempt++ {
		// Handle request based on whether headers are needed
		if len(headers) == 0 {
			response, err = client.Get(apiUrl)
		} else {
			req, _ := http.NewRequest("GET", apiUrl, nil)
			for key, value := range headers {
				req.Header.Set(key, value)
			}
			response, err = client.Do(req)
		}

		if err != nil || response.StatusCode != http.StatusOK {
			return nil, errors.New("no numbers available. Please try a different server")
		}

		// Read response body
		buf := new(strings.Builder)
		_, err = io.Copy(buf, response.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read response: %w", err)
		}
		responseData = buf.String()

		if responseData == "" {
			return nil, errors.New("no numbers available. Please try a different server")
		}

		// Parse response data
		data, err := handleResponseData(server, responseData)
		if err == nil {
			retry = false
			return data, nil
		} else {
			if attempt == 1 {
				return nil, errors.New("no numbers available. Please try different server")
			}
		}
	}
	return nil, errors.New("no numbers available after retries")
}

func HandleGetOtp(c echo.Context) error {
	ctx := context.Background()

	id := c.QueryParam("id")
	apiKey := c.QueryParam("api_key")
	server := c.QueryParam("server")

	if id == "" || apiKey == "" || server == "" {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "All fields (id, api_key, server) are required"})
	}

	client := c.Get("db").(*mongo.Client)
	db := client.Database("your_database_name")

	var apiWalletUser models.ApiWalletUser
	err := db.Collection("ApiWalletUser").FindOne(ctx, bson.M{"api_key": apiKey}).Decode(&apiWalletUser)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid API key"})
	}

	serverData, err := getServerDataWithMaintenanceCheck(ctx, db, server)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid server or maintenance issue"})
	}

	// construct api url and headers
	constructedRequest, err := constructOtpUrl(server, serverData.APIKey, id)
	if err != nil {
		return c.JSON(http.StatusBadRequest, map[string]string{"error": "Invalid server or maintenance issue"})
	}

	validOtp, err := fetchOTP(constructedRequest.URL, server, constructedRequest.Headers)
	if err != nil {
		logs.Logger.Error(err)
		return c.JSON(http.StatusInternalServerError, map[string]string{"error": "Failed to fetch API response"})
	}

	// Save transaction history logic here...
	// Process the transaction here

	// Respond with the extracted OTP
	return c.JSON(http.StatusOK, map[string]string{"otp": validOtp})
}

func HandleCheckOTP(c echo.Context) error {
	return nil
}

func HandleNumberCancel(c echo.Context) error {
	return nil
}

// Helper functions

// processTransaction handles the transaction logic
func processTransaction(collection *mongo.Collection, validOtp, id, server, userID, userEmail string, ipDetails string) error {
	// Check if the entry with the same ID and OTP already exists
	var existingEntry models.TransactionHistory
	err := collection.FindOne(context.TODO(), bson.M{"id": id, "otp": validOtp}).Decode(&existingEntry)
	if err == mongo.ErrNoDocuments {
		// Fetch the transaction details
		var transaction models.TransactionHistory
		err = collection.FindOne(context.TODO(), bson.M{"id": id}).Decode(&transaction)
		if err != nil {
			return fmt.Errorf("transaction not found: %w", err)
		}

		// Format current date and time
		currentTime := time.Now().Format("01/02/2006T03:04:05 PM")

		// // Fetch IP details
		// ipDetails, err := utils.GetIpDetails(c)
		// if err != nil {
		// 	return fmt.Errorf("failed to fetch IP details: %w", err)
		// }

		// // Format IP details as a multiline string
		// ipDetailsString := fmt.Sprintf(
		// 	"\nCity: %s\nState: %s\nPincode: %s\nCountry: %s\nService Provider: %s\nIP: %s",
		// 	ipDetails.City, ipDetails.State, ipDetails.Pincode, ipDetails.Country, ipDetails.ServiceProvider, ipDetails.IP,
		// )

		// Create a new transaction history entry
		numberHistory := models.TransactionHistory{
			UserID:        userID,
			Service:       transaction.Service,
			Price:         transaction.Price,
			Server:        server,
			ID:            primitive.NewObjectID(),
			TransactionID: id,
			OTP:           validOtp,
			Status:        "FINISHED",
			Number:        transaction.Number,
			DateTime:      currentTime,
		}

		// Save the new entry to the database
		_, err = collection.InsertOne(context.TODO(), numberHistory)
		if err != nil {
			return fmt.Errorf("failed to save transaction history: %w", err)
		}

		// Send OTP details
		err := lib.OtpGetDetails(
			userEmail,
			transaction.Service,
			transaction.Price,
			server,
			transaction.Number,
			validOtp,
			ipDetails,
		)
		if err != nil {
			return fmt.Errorf("failed to send OTP details: %w", err)
		}

		logs.Logger.Info("Transaction history and OTP details processed successfully.")
		return nil
	} else if err != nil {
		return fmt.Errorf("failed to check existing entry: %w", err)
	}

	logs.Logger.Info("Transaction already exists. Skipping.")
	return nil
}

func getServerDataWithMaintenanceCheck(ctx context.Context, db *mongo.Database, server string) (models.Server, error) {
	var serverData models.Server
	collection := models.InitializeServerCollection(db)
	err := collection.FindOne(ctx, bson.M{"server": server}).Decode(&serverData)
	if err != nil {
		return models.Server{}, err
	}
	if serverData.Maintenance == true {
		return models.Server{}, fmt.Errorf("server is under maintainance")
	}
	return serverData, nil
}

func fetchOTP(apiUrl, server string, headers map[string]string) (string, error) {
	req, err := http.NewRequest("GET", apiUrl, nil)
	if err != nil {
		return "", err
	}

	for key, value := range headers {
		req.Header.Set(key, value)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	responseData := string(body)
	validOtp, parseErr := parseResponse(server, responseData)
	if parseErr != nil {
		return "", fmt.Errorf("failed to parse response: %w", parseErr)
	}

	return validOtp, nil
}

func parseResponse(server string, responseData string) (string, error) {
	// Try to handle JSON response first
	var jsonData map[string]interface{}
	err := json.Unmarshal([]byte(responseData), &jsonData)
	if err == nil {
		// JSON parsing succeeded
		switch server {
		case "2":
			if smsList, ok := jsonData["sms"].([]interface{}); ok && len(smsList) > 0 {
				// Assume each SMS has a "text" and "date" field
				latestSms := smsList[0].(map[string]interface{})
				if text, ok := latestSms["text"].(string); ok {
					return text, nil
				}
			}
		case "7", "8":
			if smsCode, ok := jsonData["sms_code"].(string); ok {
				return smsCode, nil
			}
		case "9":
			if data, ok := jsonData["data"].(map[string]interface{}); ok {
				if vcList, ok := data["verificationCode"].([]interface{}); ok && len(vcList) > 0 {
					if vc, ok := vcList[0].(map[string]interface{}); ok {
						if code, ok := vc["vc"].(string); ok {
							return code, nil
						}
					}
				}
			}
		}
	}

	// Handle string responses if JSON parsing fails
	if strings.HasPrefix(responseData, "STATUS_OK") {
		parts := strings.Split(responseData, ":")
		if len(parts) > 1 {
			return strings.TrimSpace(parts[1]), nil
		}
	}

	return "", errors.New("unknown response format")
}

func constructOtpUrl(server, apiKeyServer, id string) (ApiRequest, error) {
	var request ApiRequest
	request.Headers = make(map[string]string)

	switch server {
	case "1":
		request.URL = fmt.Sprintf("https://fastsms.su/stubs/handler_api.php?api_key=%s&action=getStatus&id=%s", apiKeyServer, id)
	case "2":
		request.URL = fmt.Sprintf("https://5sim.net/v1/user/check/%s", id)
		request.Headers["Authorization"] = fmt.Sprintf("Bearer %s", apiKeyServer)
		request.Headers["Accept"] = "application/json"
	case "3":
		request.URL = fmt.Sprintf("https://smshub.org/stubs/handler_api.php?api_key=%s&action=getStatus&id=%s", apiKeyServer, id)
	case "4":
		request.URL = fmt.Sprintf("https://api.tiger-sms.com/stubs/handler_api.php?api_key=%s&action=getStatus&id=%s", apiKeyServer, id)
	case "5":
		request.URL = fmt.Sprintf("https://api.grizzlysms.com/stubs/handler_api.php?api_key=%s&action=getStatus&id=%s", apiKeyServer, id)
	case "6":
		request.URL = fmt.Sprintf("https://tempnum.org/stubs/handler_api.php?api_key=%s&action=getStatus&id=%s", apiKeyServer, id)
	case "7", "8":
		request.URL = fmt.Sprintf("https://api2.sms-man.com/control/get-sms?token=%s&request_id=%s", apiKeyServer, id)
	case "9":
		request.URL = fmt.Sprintf("http://www.phantomunion.com:10023/pickCode-api/push/sweetWrapper?token=%s&serialNumber=%s", apiKeyServer, id)
	default:
		return ApiRequest{}, errors.New("invalid server value")
	}

	return request, nil
}
