// Package playerq is a player queue specific redis implementation and will be removed in a future version.
/*
Copyright 2018 Google LLC

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.

*/
package playerq

import (
	"encoding/json"
	"strings"
	"time"

	"github.com/gomodule/redigo/redis"
	log "github.com/sirupsen/logrus"
)

// Logrus structured logging setup
var (
	pqLogFields = log.Fields{
		"app":       "openmatch",
		"component": "redishelpers",
		"caller":    "statestorage/redis/redishelpers.go",
	}
	pqLog = log.WithFields(pqLogFields)
)

func indicesMap(results []string) interface{} {
	indices := make(map[string][]string)
	for _, iName := range results {
		field := strings.Split(iName, ":")
		indices[field[0]] = append(indices[field[0]], field[1])
	}
	return indices
}

// PlayerIndices retrieves available indices for player parameters.
func playerIndices(redisConn redis.Conn) (results []string, err error) {
	results, err = redis.Strings(redisConn.Do("SMEMBERS", "indices"))
	return
}

// Create adds a player's JSON representation to the current matchmaker state storage,
// and indexes all fields in that player's JSON object. All values in the JSON should be integers.
// If you're trying to index a boolean, just use the epoch timestamp of the
// request as the value; the existance of that value for this group/player can
// be considered a 'true' value.
// Example:
// player {
//   "ping.us-east": 70,
//   "ping.eu-central": 120,
//   "map.sunsetvalley": "123456782", // TRUE flag key, epoch timestamp value
//   "mode.ctf" // TRUE flag key, epoch timestamp value
// }
// TODO: Make indexing more customizable.
func Create(redisConn redis.Conn, playerID string, playerData string, queueID string) (err error) {
	//pdJSON, err := json.Marshal(playerData)
	pdMap := redisValuetoMap(playerData)
	check(err, "")

	redisConn.Send("MULTI")
	redisConn.Send("HSET", playerID, "properties", playerData)
	for key, value := range pdMap {
		// TODO: walk the JSON and flatten it
		// Index this property
		redisConn.Send("ZADD", key, value, playerID)
		// Add this index to the list of indices
		redisConn.Send("SADD", "indices", key)
	}

	if queueID != "" {
		redisConn.Send("ZINCRBY", "queues", 1, queueID)
	}

	// Add the player to the built-in indexes
	redisConn.Send("SADD", "indices", "timestamp")
	redisConn.Send("ZADD", "timestamp", int(time.Now().Unix()), playerID)

	_, err = redisConn.Do("EXEC")
	return
}

// Update is an alias for Create() in this implementation
func Update(redisConn redis.Conn, playerID string, playerData string) (err error) {
	Create(redisConn, playerID, playerData, "")
	return
}

// Retrieve a player's JSON object representation from state storage.
func Retrieve(redisConn redis.Conn, playerID string) (results map[string]interface{}, err error) {
	r, err := redis.String(redisConn.Do("HGET", playerID, "properties"))
	if err != nil {
		log.Println("Failed to get properties from playerID using HGET", err)
	}
	results = redisValuetoMap(r)
	return
}

// Convert redis result (JSON blob in a string) to golang map
func redisValuetoMap(result string) map[string]interface{} {
	jsonPD := make(map[string]interface{})
	byt := []byte(result)
	err := json.Unmarshal(byt, &jsonPD)
	check(err, "")
	return jsonPD
}

// Delete a player's JSON object representation from state storage,
// and attempt to remove the player's presence in any indexes.
func Delete(redisConn redis.Conn, playerID string, queueID string) (err error) {
	results, err := Retrieve(redisConn, playerID)
	redisConn.Send("MULTI")
	redisConn.Send("DEL", playerID)

	// Remove playerID from generated indices
	for iName := range results {
		log.WithFields(log.Fields{
			"field": iName,
			"key":   playerID}).Debug("Indexing field")
		redisConn.Send("ZREM", iName, playerID)
	}

	if queueID != "" {
		redisConn.Send("ZINCRBY", "queues", -1, queueID)
	}

	// Remove the playerID from the built-in indexes
	redisConn.Send("ZREM", "timestamp", playerID)

	_, err = redisConn.Do("EXEC")
	check(err, "")
	return
}

// Unindex a player without deleting their JSON object representation from
// state storage.
func Unindex(redisConn redis.Conn, playerID string) (err error) {
	results, err := Retrieve(redisConn, playerID)
	if err != nil {
		log.Println("couldn't retreive player properties for ", playerID)
	}

	redisConn.Send("MULTI")

	// Remove playerID from the generated indices
	for iName := range results {
		log.WithFields(log.Fields{
			"field": iName,
			"key":   playerID}).Debug("Un-indexing field")
		redisConn.Send("ZREM", iName, playerID)
	}

	// Remove the playerID from the built-in indexes
	redisConn.Send("ZREM", "timestamp", playerID)

	_, err = redisConn.Do("EXEC")
	check(err, "")
	return

}

func check(err error, action string) {
	if err != nil {
		if action == "QUIT" {
			log.Fatal(err)
		} else {
			log.Print(err)
		}
	}
}
