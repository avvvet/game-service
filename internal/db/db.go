package db

import (
	"context"
	"log"
	"net/url"
	"os"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func ConnectToDB() (*mongo.Database, context.CancelFunc, error) {
	mongoURI := os.Getenv("MONGODB_URI")

	uri, err := url.Parse(mongoURI)
	if err != nil {
		log.Fatalf("Error parsing MongoDB URI: %v", err)
		return nil, nil, err
	}

	dbName := strings.TrimPrefix(uri.Path, "/")

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)

	client, err := mongo.Connect(ctx, options.Client().ApplyURI(mongoURI))
	if err != nil {
		log.Fatalf("Error connecting to MongoDB: %v", err)
		return nil, nil, err
	}

	if err := client.Ping(ctx, nil); err != nil {
		log.Fatalf("Error pinging MongoDB: %v", err)
		return nil, nil, err
	}

	db := client.Database(dbName)

	return db, cancel, nil
}

func GetTransactionClient() (*mongo.Client, error) {

	mongoURI := os.Getenv("MONGODB_URI")
	clientOptions := options.Client().ApplyURI(mongoURI)

	client, err := mongo.Connect(context.Background(), clientOptions)
	if err != nil {
		return nil, err
	}

	return client, nil
}

func CreateTTLIndexForCollection(db *mongo.Database, collectionName string) {
	collection := db.Collection(collectionName)

	// Define the TTL index
	indexModel := mongo.IndexModel{
		Keys:    bson.M{"expires_at": 1},
		Options: options.Index().SetExpireAfterSeconds(0), // 0 means that MongoDB will calculate the TTL based on the `ExpiresAt` field.
	}

	// Create the TTL index
	_, err := collection.Indexes().CreateOne(context.TODO(), indexModel)
	if err != nil {
		log.Fatal(err)
	}
}
