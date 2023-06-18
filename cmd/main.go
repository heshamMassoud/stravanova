package main

import (
	"bytes"
	"cloud.google.com/go/cloudsqlconn"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"
	"io"
	"log"
	"math"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"
)

const (
	redirectURI = "https://stratonova-l5snujqyaq-ew.a.run.app"
	AthleteID   = 13560298
)

type AccessTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type Workout struct {
	ID                 int       `json:"id"`
	Name               string    `json:"name"`
	Distance           float64   `json:"distance"`
	TotalElevationGain float64   `json:"total_elevation_gain"`
	Duration           int       `json:"moving_time"`
	Laps               []Lap     `json:"laps"`
	StartLocation      []float64 `json:"start_latlng"`
	AverageSpeed       float64   `json:"average_speed"`
}

type Lap struct {
	MaxSpeed         float64 `json:"max_speed"`
	AverageSpeed     float64 `json:"average_speed"`
	AverageCadence   float64 `json:"average_cadence"`
	AverageHeartRate float64 `json:"average_heartrate"`
}

func main() {
	// Define your handlers for different endpoints
	http.HandleFunc("/", mainPageHandler)
	http.HandleFunc("/exchange_token", exchangeTokenHandler)
	http.HandleFunc("/update_workout", updateActivityHandler)
	http.HandleFunc("/token", tokenHandler)
	http.HandleFunc("/webhook", webhookHandler)

	// Start the HTTP server
	err := http.ListenAndServe(":8080", nil)
	if err != nil {
		fmt.Println("Error starting server:", err)
	}
}

func mainPageHandler(w http.ResponseWriter, r *http.Request) {
	stravaClientID := os.Getenv("STRAVA_CLIENT_ID")
	// Step 1: Redirect the user to the Strava authorization page
	authURL := fmt.Sprintf("https://www.strava.com/oauth/authorize?client_id=%s&response_type=code&scope=activity:read_all,activity:write&approval_prompt=force&redirect_uri=%s/exchange_token", stravaClientID, redirectURI)
	fmt.Fprintf(w, "In case you do not have an access token, please visit the following URL to authorize the application: %s", authURL)
	fmt.Fprintf(w, "otherwise, you can already start using the app by visiting the following URL: %s", "https://stratonova-l5snujqyaq-ew.a.run.app/update-activity?access_token={access token}")
}

func tokenHandler(w http.ResponseWriter, r *http.Request) {
	fmt.Println("Attempting to fetch token from cloud sql.")
	token := getAccessToken()
	fmt.Fprintf(w, "Successfuly got an access token : %s", token)
}

func exchangeTokenHandler(w http.ResponseWriter, r *http.Request) {
	// Example of capturing the authorization code from a simulated redirect URI
	authorizationCode := r.URL.Query().Get("code")
	fmt.Println("Successfully got the auth code 🎉", authorizationCode)

	// Step 3: Exchange the authorization code for an access token
	accessToken, err := exchangeCodeForToken(authorizationCode)
	if err != nil {
		fmt.Println("Failed to exchange authorization code for access token:", err)
		return
	}
	fmt.Fprintf(w, "Successfully got an accessToken 🎉%s", accessToken)
}

func updateActivityHandler(w http.ResponseWriter, r *http.Request) {
	accessToken := getAccessToken()
	workoutID, err := strconv.Atoi(r.URL.Query().Get("workout_id"))

	if err != nil {
		fmt.Fprintf(w, "Invalid activity id 🙃🙃🙃: %s", err)
	}

	workout, err := fetchWorkoutDetails(workoutID, accessToken)
	if err != nil {
		fmt.Println("Failed to fetch workout details", err)
		return
	}
	fmt.Fprintf(w, "Successfully fetched the workout 🎉 %s", workout.Name)

	// Print the workout details
	fmt.Fprintf(w, "Workout ID: %s", workout.ID)
	fmt.Fprintf(w, "Workout Name: %s", workout.Name)
	fmt.Fprintf(w, "Distance: %s", workout.Distance)
	fmt.Fprintf(w, "Elevation Gain: %s", workout.TotalElevationGain)
	fmt.Fprintf(w, "Duration: %s", workout.Duration)

	fmt.Println("Updating workout description..")

	prompt := buildPrompt(workout)
	fmt.Printf("Sending this prompt to chatgpt: %s\n", prompt)
	summary, err := generateSummary(prompt)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Printf("Summary from chatgpt: %s\n", summary)

	err = updateWorkout(workoutID, summary, generateActivityName(workout), accessToken)
	if err != nil {
		fmt.Println("Failed to update workout description:", err)
		return
	}
	fmt.Fprintf(w, "Workout description updated successfully!")
}

func generateActivityName(workout Workout) string {
	const EasyRunThreshold = 8000
	if workout.Distance < EasyRunThreshold {
		return "Short but Sweet 💁🏽‍♂️"
	}

	const LongRunThreshold = 15000
	if workout.Distance > LongRunThreshold {
		return "Long Run ☄️"
	}

	// Get 2 laps from mid run and compare the pace, if its more than 2 mins, its def interval.
	totalLaps := len(workout.Laps)
	totalKms := convertMetersToKilometers(workout.Distance)

	if hasMoreLapsThanKms(totalLaps, totalKms) {
		// its either interval or threshold or progressive
		if isIntervalTraining(workout) {
			return "Interval training 💪🛤️"
		}
		return "Threshold Training 🚀🚀🚀"
	}

	return "Easy Flow 🌊🌊"
}

func hasMoreLapsThanKms(totalLaps int, totalKms float64) bool {
	return totalLaps > int(totalKms)
}

func isIntervalTraining(workout Workout) bool {
	totalLaps := len(workout.Laps)
	midLap := totalLaps / 2
	midLap1 := workout.Laps[midLap]
	midLap2 := workout.Laps[midLap+1]
	return isSpeedJump(midLap1, midLap2)
}

func isSpeedJump(lap1 Lap, lap2 Lap) bool {
	return math.Abs(lap1.AverageSpeed-lap2.AverageSpeed) > 2
}

func convertMetersToKilometers(meters float64) float64 {
	kilometers := math.Ceil(meters / 1000)
	return kilometers
}

func prettyPrintJSON(jsonStr string) {
	var jsonData interface{}
	err := json.Unmarshal([]byte(jsonStr), &jsonData)
	if err != nil {
		fmt.Println("Failed to unmarshal JSON:", err)
		return
	}

	prettyJSON, err := json.MarshalIndent(jsonData, "", "  ")
	if err != nil {
		fmt.Println("Failed to marshal JSON:", err)
		return
	}

	fmt.Println(string(prettyJSON))
}

func exchangeCodeForToken(code string) (string, error) {
	// Create a new HTTP client
	client := http.Client{}

	// Create a POST request to exchange the authorization code for an access token
	req, err := http.NewRequest("POST", "https://www.strava.com/oauth/token", nil)
	if err != nil {
		return "", err
	}

	// Set the request parameters
	params := req.URL.Query()
	params.Add("client_id", os.Getenv("STRAVA_CLIENT_ID"))
	params.Add("client_secret", os.Getenv("STRAVA_CLIENT_SECRET"))
	params.Add("code", code)
	params.Add("grant_type", "authorization_code")
	req.URL.RawQuery = params.Encode()

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	prettyPrintJSON(string(body))

	fmt.Println("Successfully fetched token for athlete: 🎉", string(body))

	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("request failed with status: %d, response: %s", resp.StatusCode, string(body))
	}

	// Parse the response body to get the access token
	var tokenResp AccessTokenResponse
	err = json.Unmarshal(body, &tokenResp)
	if err != nil {
		return "", err
	}

	return tokenResp.AccessToken, nil
}

func fetchWorkoutDetails(workoutID int, accessToken string) (Workout, error) {
	fmt.Printf("Fetching workout: %d with access token: %s", workoutID, accessToken)
	// Create a new HTTP client
	client := http.Client{}

	// Create a GET request to fetch the workout details
	req, err := http.NewRequest("GET", fmt.Sprintf("https://www.strava.com/api/v3/activities/%d", workoutID), nil)
	if err != nil {
		return Workout{}, err
	}

	// Set the access token in the request header
	req.Header.Set("Authorization", "Bearer "+accessToken)

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return Workout{}, err
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return Workout{}, err
	}

	prettyPrintJSON(string(body))

	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		return Workout{}, fmt.Errorf("request failed with status: %d, response: %s", resp.StatusCode, string(body))
	}

	// Parse the response body into a Workout struct
	var workout Workout
	err = json.Unmarshal(body, &workout)
	if err != nil {
		return Workout{}, err
	}

	return workout, nil
}

func updateWorkout(workoutID int, newDescription string, newName string, accessToken string) error {
	// Create a new HTTP client
	client := http.Client{}

	// Create a PUT request to update the workout description
	req, err := http.NewRequest("PUT", fmt.Sprintf("https://www.strava.com/api/v3/activities/%d", workoutID), nil)
	if err != nil {
		return err
	}

	// Set the access token in the request header
	req.Header.Set("Authorization", "Bearer "+accessToken)

	// Set the new description in the request body
	req.Header.Set("Content-Type", "application/json")
	payload := map[string]interface{}{
		"description": newDescription,
		"name":        newName,
	}
	jsonPayload, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req.Body = io.NopCloser(bytes.NewReader(jsonPayload))

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("request failed with status: %d", resp.StatusCode)
	}

	return nil
}

type OpenAIRequest struct {
	Messages []Message `json:"messages"`
	Model    string    `json:"model"`
}

type Message struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

type OpenAIResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func generateSummary(prompt string) (string, error) {
	apiKey := mustGetEnv("OPENAI_API_KEY")
	url := "https://api.openai.com/v1/chat/completions"

	message := Message{
		Content: prompt,
		Role:    "user",
	}

	messages := []Message{message}
	requestBody, err := json.Marshal(OpenAIRequest{
		Model:    "gpt-3.5-turbo",
		Messages: messages,
	})
	if err != nil {
		return "", err
	}

	client := &http.Client{}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(requestBody))
	if err != nil {
		return "", err
	}

	req.Header.Add("Content-Type", "application/json")
	req.Header.Add("Authorization", fmt.Sprintf("Bearer %s", apiKey))

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}

	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	var openAIResp OpenAIResponse
	err = json.Unmarshal(body, &openAIResp)
	if err != nil {
		return "", err
	}

	if len(openAIResp.Choices) > 0 {
		return openAIResp.Choices[0].Message.Content, nil
	}

	return "", fmt.Errorf("No response received from ChatGPT")
}

func buildPrompt(workout Workout) string {
	name := generateActivityName(workout)
	kilometers := convertMetersToKilometers(workout.Distance)
	duration := workout.Duration
	elevationGain := workout.TotalElevationGain
	latitude := workout.StartLocation[0]
	longitude := workout.StartLocation[1]
	averageSpeed := workout.AverageSpeed

	return fmt.Sprintf(`
	Based on the following information about a run I just did:
	name: %s,
	distance in kms: %f,
	average speed: %f, (please convert that to km pace instead of speed)
	duration in seconds: %d (please mention that in a human friendly format)
	elevationGain: %f (only if that was high, mention that the run was hilly)

	given the following latitude: %f and longitude: %f, mention where the run was done city and district wise 
	(as if you are a local from this city)
	
	Please generate a summary in a story-telling exciting way rather
	than a list. The story should be talking to me e.g. "You ..."
		
	Put all this information in one paragraph in a witty and concise way.`, name, kilometers, averageSpeed, duration, elevationGain, latitude, longitude)
}

func openAI() {
	const prompt = `
	Based on the JSON I provied earlier, can you please generate a summary in a story-telling exciting way rather
	than a list. The name of the athlete who did the run is called Hesham. So you can output it a la "hesham went for a
	run .....". use Kms instead of meters. And for the elevation detail just mention if it was overall a hilly run 
	or not

	. For the average speed, use pace instead

	Can I see a variation of this, instead of talking about Hesham. It talks to me with "You ..."

	The most important things I really like to know is how much effort was it for me compared to my
		history as a runner. The weather. The route itself. How much time it took. The distance. And my pace. 
		Put all this information in one paragraph in a funny concise way`
}

type AccessToken struct {
	AthleteId int
	Token     string
	ExpiresAt time.Time
}

func getAccessToken() string {
	// Open a connection to the database
	db, err := connectWithConnector()
	if err != nil {
		log.Fatal(err)
		fmt.Println("failed to open con!", err)
	}
	defer db.Close()

	// Test the connection
	err = db.Ping()
	if err != nil {
		log.Fatal(err)
		fmt.Println("failed to ping!", err)
	}

	fmt.Println("Connected to the database!")

	// Query a row from the access_tokens table
	var accessToken AccessToken
	query := fmt.Sprintf("SELECT *  FROM strava_access_tokens WHERE athlete_id=%d;", AthleteID)
	err = db.QueryRow(query).Scan(&accessToken.AthleteId, &accessToken.Token, &accessToken.ExpiresAt)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Access Token: AthleteId=%d, Token=%s, ExpiresAt=%s\n", accessToken.AthleteId, accessToken.Token, accessToken.ExpiresAt)

	return accessToken.Token
}

func connectWithConnector() (*sql.DB, error) {
	// Note: Saving credentials in environment variables is convenient, but not
	// secure - consider a more secure solution such as
	// Cloud Secret Manager (https://cloud.google.com/secret-manager) to help
	// keep passwords and other secrets safe.
	var (
		dbUser                 = mustGetEnv("DB_USER")                  // e.g. 'my-db-user'
		dbPwd                  = mustGetEnv("DB_PASS")                  // e.g. 'my-db-password'
		dbName                 = mustGetEnv("DB_NAME")                  // e.g. 'my-database'
		instanceConnectionName = mustGetEnv("INSTANCE_CONNECTION_NAME") // e.g. 'project:region:instance'
		usePrivate             = os.Getenv("PRIVATE_IP")
	)

	d, err := cloudsqlconn.NewDialer(context.Background())
	if err != nil {
		return nil, fmt.Errorf("cloudsqlconn.NewDialer: %w", err)
	}
	var opts []cloudsqlconn.DialOption
	if usePrivate != "" {
		opts = append(opts, cloudsqlconn.WithPrivateIP())
	}
	mysql.RegisterDialContext("cloudsqlconn",
		func(ctx context.Context, addr string) (net.Conn, error) {
			return d.Dial(ctx, instanceConnectionName, opts...)
		})

	dbURI := fmt.Sprintf("%s:%s@cloudsqlconn(localhost:3306)/%s?parseTime=true",
		dbUser, dbPwd, dbName)

	dbPool, err := sql.Open("mysql", dbURI)
	if err != nil {
		return nil, fmt.Errorf("sql.Open: %w", err)
	}
	return dbPool, nil
}

func mustGetEnv(k string) string {
	v := os.Getenv(k)
	if v == "" {
		log.Fatalf("Fatal Error in connect_connector.go: %s environment variable not set.", k)
	}
	return v
}

type WebhookEvent struct {
	ObjectType string `json:"object_type"`
	ObjectId   string `json:"object_id"`
}

func webhookHandler(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "GET":
		verifyToken := mustGetEnv("STRAVA_VERIFY_TOKEN")
		// Parses the query params
		mode := r.URL.Query().Get("hub.mode")
		token := r.URL.Query().Get("hub.verify_token")
		challenge := r.URL.Query().Get("hub.challenge")

		if mode != "" && token != "" {
			if mode == "subscribe" && token == verifyToken {
				fmt.Println("WEBHOOK_VERIFIED")
				w.WriteHeader(http.StatusOK)
				w.Header().Set("Content-Type", "application/json")
				resp := make(map[string]string)
				resp["hub.challenge"] = challenge
				jsonResp, err := json.Marshal(resp)
				if err != nil {
					log.Fatalf("Error happened in JSON marshal. Err: %s", err)
				}
				fmt.Println(jsonResp)
				w.Write(jsonResp)
				return
			} else {
				http.Error(w, "403 Forbidden", http.StatusForbidden)
			}
		}
	case "POST":
		// Read the request body
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "Failed to read request body", http.StatusInternalServerError)
			return
		}

		// Close the request body to prevent resource leaks
		defer r.Body.Close()

		// Parse the JSON data into a struct
		var event WebhookEvent
		err = json.Unmarshal(body, &event)
		if err != nil {
			http.Error(w, "Failed to parse JSON data", http.StatusBadRequest)
			return
		}
		fmt.Printf("ObjectType %s", event.ObjectType)
		fmt.Printf("ObjectId %s", event.ObjectId)

		if event.ObjectType == "activity" {
			accessToken := getAccessToken()
			workoutID, err := strconv.Atoi(event.ObjectId)

			if err != nil {
				fmt.Printf("invalid activity id")
			}
			workout, err := fetchWorkoutDetails(workoutID, accessToken)
			prompt := buildPrompt(workout)
			fmt.Printf("Sending this prompt to chatgpt: %s\n", prompt)
			summary, err := generateSummary(prompt)
			if err != nil {
				fmt.Println("Error:", err)
				return
			}
			fmt.Printf("Summary from chatgpt: %s\n", summary)

			err = updateWorkout(workoutID, summary, generateActivityName(workout), accessToken)
			if err != nil {
				fmt.Println("Failed to update workout description:", err)
				return
			}
			fmt.Fprintf(w, "Workout description updated successfully!")

		} else {
			http.Error(w, "Webhook event not supported yet.", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "Sorry, only GET and POST are supported", http.StatusNotFound)
	}
}
