package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"strconv"
	"time"

	"github.com/ranjankuldeep/fakeNumber/internal/database/models"
	"github.com/ranjankuldeep/fakeNumber/logs"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

type ServerDataUpload struct {
	Server int    `bson:"server" json:"server"`
	Price  string `bson:"price" json:"price"`
	Code   string `bson:"code" json:"code"`
	Otp    string `bson:"otp" json:"otp"`
}

type ServerListUpload struct {
	ID           primitive.ObjectID `bson:"_id,omitempty"`
	Name         string             `bson:"name" json:"name"`
	Service_Code string             `bson:"service_code" json:"service_code"`
	Servers      []ServerDataUpload `bson:"servers" json:"servers"`
	CreatedAt    time.Time          `bson:"createdAt,omitempty" json:"createdAt"`
	UpdatedAt    time.Time          `bson:"updatedAt,omitempty" json:"updatedAt"`
}

func FetchServerData(url string) ([]ServerListUpload, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch data: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var serverData []ServerListUpload
	if err := json.Unmarshal(body, &serverData); err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}
	return serverData, nil
}

func FetchMarginAndExchangeRate(ctx context.Context, db *mongo.Database) (map[int]float64, map[int]float64, error) {
	serverCollection := models.InitializeServerCollection(db)
	marginMap := make(map[int]float64)
	exchangeRateMap := make(map[int]float64)

	cursor, err := serverCollection.Find(ctx, bson.M{})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to fetch servers: %w", err)
	}
	defer cursor.Close(ctx)

	// Iterate over the fetched servers and populate the maps
	for cursor.Next(ctx) {
		var server models.Server
		if err := cursor.Decode(&server); err != nil {
			return nil, nil, fmt.Errorf("failed to decode server: %w", err)
		}
		marginMap[server.ServerNumber] = server.Margin
		exchangeRateMap[server.ServerNumber] = server.ExchangeRate
	}

	if err := cursor.Err(); err != nil {
		return nil, nil, fmt.Errorf("error while iterating over servers: %w", err)
	}
	return marginMap, exchangeRateMap, nil
}

func UpdateServerData(db *mongo.Database, ctx context.Context) error {
	url := "https://own5k.in/p/final.php"
	serverData, err := FetchServerData(url)
	if err != nil {
		logs.Logger.Error(err)
		return err
	}
	marginMap, exchangeMap, err := FetchMarginAndExchangeRate(ctx, db)
	if err != nil {
		logs.Logger.Error(err)
		return err
	}
	logs.Logger.Info(marginMap)
	logs.Logger.Info(exchangeMap)

	for serviceIndex, service := range serverData {
		for serverIndex, server := range service.Servers {
			priceFloat, err := strconv.ParseFloat(server.Price, 64)
			if err != nil {
				fmt.Printf("Invalid price for server %d: %v\n", server.Server, err)
				continue
			}
			serverData[serviceIndex].Servers[serverIndex].Price = fmt.Sprintf("%.2f", priceFloat*exchangeMap[server.Server]+marginMap[server.Server])
		}
	}

	serverListCollection := models.InitializeServerListCollection(db)
	_, err = serverListCollection.DeleteMany(ctx, bson.M{})
	if err != nil {
		return fmt.Errorf("failed to clear server list collection: %w", err)
	}

	var documents []interface{}
	for _, data := range serverData {
		data.CreatedAt = time.Now()
		data.UpdatedAt = time.Now()
		documents = append(documents, data)
	}
	_, err = serverListCollection.InsertMany(ctx, documents)
	if err != nil {
		return fmt.Errorf("failed to insert data in batch: %w", err)
	}
	return nil
}
