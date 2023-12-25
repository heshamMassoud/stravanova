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
	"strings"
	"time"
)

const (
	redirectURI = "https://stratonova-l5snujqyaq-ew.a.run.app"
	AthleteID   = 13560298
)

type AccessTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresAt    int64  `json:"expires_at"`
}

type Workout struct {
	ID                 int       `json:"id"`
	Name               string    `json:"name"`
	SportType          string    `json:"sport_type"`
	Distance           float64   `json:"distance"`
	TotalElevationGain float64   `json:"total_elevation_gain"`
	Duration           int       `json:"moving_time"`
	Laps               []Lap     `json:"laps"`
	StartLocation      []float64 `json:"start_latlng"`
	AverageSpeed       float64   `json:"average_speed"`
	Date               time.Time `json:"start_date"`
	HeartRate          float64   `json:"average_heartrate"`
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
	accessToken, err := getTokenFromStrava(authorizationCode, "")
	if err != nil {
		fmt.Println("Failed to exchange authorization code for access token:", err)
		return
	}
	fmt.Fprintf(w, "Successfully got an accessToken 🎉%s", accessToken.AccessToken)
}

func updateActivityHandler(w http.ResponseWriter, r *http.Request) {
	accessToken := getAccessToken()

	workoutID, err := strconv.Atoi(r.URL.Query().Get("workout_id"))
	if err != nil {
		fmt.Fprintf(w, "Invalid activity id 🙃🙃🙃: %s", err)
	}

	workouts, err := fetchWeekWorkouts(w, accessToken)
	if err != nil {
		fmt.Println("Failed to fetch workout details", err)
		return
	}

	prompt := buildPrompt(workouts)
	fmt.Printf("Sending this prompt to chatgpt: %s\n", prompt)
	summary, err := generateSummary(prompt)
	if err != nil {
		fmt.Println("Error:", err)
		return
	}
	fmt.Printf("Summary from chatgpt: %s\n", summary)

	err = updateWorkout(workoutID, summary, "Week Finisher ☄️", accessToken)
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

	if hasMoreLapsThanKms(totalLaps, math.Ceil(totalKms)) {
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
	kilometers := meters / 1000.0
	rounded := math.Round(kilometers*10) / 10.0
	return rounded
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

func getTokenFromStrava(code string, refreshToken string) (AccessTokenResponse, error) {
	// Create a new HTTP client
	client := http.Client{}

	// Create a POST request to exchange the authorization code for an access token
	req, err := http.NewRequest("POST", "https://www.strava.com/oauth/token", nil)
	if err != nil {
		return AccessTokenResponse{}, err
	}

	// Set the request parameters
	params := req.URL.Query()
	params.Add("client_id", os.Getenv("STRAVA_CLIENT_ID"))
	params.Add("client_secret", os.Getenv("STRAVA_CLIENT_SECRET"))

	if code != "" {
		params.Add("code", code)
		params.Add("grant_type", "authorization_code")
	} else {
		params.Add("refresh_token", refreshToken)
		params.Add("grant_type", "refresh_token")
	}

	req.URL.RawQuery = params.Encode()

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return AccessTokenResponse{}, err
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return AccessTokenResponse{}, err
	}

	prettyPrintJSON(string(body))

	fmt.Println("Successfully fetched token for athlete: 🎉", string(body))

	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		return AccessTokenResponse{}, fmt.Errorf("request failed with status: %d, response: %s", resp.StatusCode, string(body))
	}

	// Parse the response body to get the access token
	var tokenResp AccessTokenResponse
	err = json.Unmarshal(body, &tokenResp)
	if err != nil {
		return AccessTokenResponse{}, err
	}

	return tokenResp, nil
}

// GetCurrentTimeEpoch returns the current time in Unix epoch seconds
func getCurrentTimeEpoch() int64 {
	return time.Now().Unix()
}

// GetLastWeekTimeEpoch returns the Unix epoch seconds for the same time last week
func getLastWeekTimeEpoch() int64 {
	// Subtract 7 days from the current time
	lastWeek := time.Now().AddDate(0, 0, -7)
	return lastWeek.Unix()
}

func fetchWeekWorkouts(w http.ResponseWriter, accessToken string) ([]Workout, error) {
	//fmt.Printf("Fetching workout: %d with access token: %s", workoutID, accessToken)
	// Create a new HTTP client
	client := http.Client{}

	// Create a GET request to fetch the workout details
	req, err := http.NewRequest("GET", fmt.Sprintf("https://www.strava.com/api/v3/activities?before=%d&after=%d", getCurrentTimeEpoch(), getLastWeekTimeEpoch()), nil)
	if err != nil {
		return []Workout{}, err
	}

	// Set the access token in the request header
	req.Header.Set("Authorization", "Bearer "+accessToken)

	// Send the request
	resp, err := client.Do(req)
	if err != nil {
		return []Workout{}, err
	}
	defer resp.Body.Close()

	// Read the response body
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return []Workout{}, err
	}

	// prettyPrintJSON(string(body))

	// Check the response status code
	if resp.StatusCode != http.StatusOK {
		return []Workout{}, fmt.Errorf("request failed with status: %d, response: %s", resp.StatusCode, string(body))
	}

	// Parse the response body into a Workout struct
	var workouts []Workout
	err = json.Unmarshal(body, &workouts)
	if err != nil {
		return []Workout{}, err
	}

	_, _ = fmt.Fprintf(w, "Successfully fetched the %d workouts 🎉 ", len(workouts))

	return workouts, nil
}

func updateWorkout(workoutID int, newDescription string, newName string, accessToken string) error {
	// Create a new HTTP client
	client := http.Client{}

	fmt.Printf("updating workout on strava with id: %d: %s\n", workoutID, newDescription)

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
		Model:    "gpt-4-1106-preview",
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
		return openAIResp.Choices[0].Message.Content +
			`	
			Your friendly neighbourhood - Stratonova ✌️🏴‍☠️
			`, nil
	}

	return "", fmt.Errorf("No response received from ChatGPT")
}

// buildPrompt creates a prompt for generating a weekly summary of workouts
func buildPrompt(workouts []Workout) string {
	var sb strings.Builder

	sb.WriteString("Generate a weekly running summary based on the following workouts:\n\n")

	for _, w := range workouts {
		sb.WriteString(fmt.Sprintf(
			"- %s on %s: %.2f km, duration %s, elevation gain %.2f meters, average heart rate %.1f bpm. \n",
			w.Name,
			w.Date.Format("Monday"),
			w.Distance/1000,
			humanReadableDuration(w.Duration),
			w.TotalElevationGain,
			w.HeartRate,
		))
	}

	sb.WriteString("\nWrite the summary in a story-telling, exciting, and motivational way, humble way suitable for " +
		"a Strava post (no need for hashtags). Don't make it cheesy." +
		" The summary should consider when I was running with people or solo. " +
		"Insights on best times of days for performance." +
		"- total weekly distance (mention that in context for what’s to come next week)\n- the summary should be" +
		" written in an engaging way for the reader - not a big chunk of text.\n" +
		"- some insights on based last week’s runs you are usually more performant at this time of" +
		" the day based on the average heart rate and effort. \n" +
		" Also the summary, should consider the grand scheme of things which is training for the berlin marathon in September 2024\n\n")

	return sb.String()
}

// humanReadableDuration converts duration from seconds to a human-friendly format
func humanReadableDuration(seconds int) string {
	hours := seconds / 3600
	minutes := (seconds % 3600) / 60
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
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

type RefreshToken struct {
	AthleteId    int
	RefreshToken string
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

	accessToken := getAccessTokenFromSQL(db, AthleteID)
	if accessToken.ExpiresAt.Before(time.Now()) {
		refreshToken := getRefreshTokenFromSQL(db, AthleteID)
		newAccessToken, err := getTokenFromStrava("", refreshToken.RefreshToken)
		if err != nil {
			log.Fatalf("Failed to refresh the token on strava. Err: %s", err)
		}
		updateTokens(db, AthleteID, newAccessToken)
	}

	fmt.Printf("Got Access Token! : AthleteId=%d, Token=%s\n", accessToken.AthleteId, accessToken.Token)

	return accessToken.Token
}

func getAccessTokenFromSQL(db *sql.DB, athleteID int) AccessToken {
	var accessToken AccessToken
	query := fmt.Sprintf("SELECT *  FROM strava_access_tokens WHERE athlete_id=%d;", athleteID)
	err := db.QueryRow(query).Scan(&accessToken.AthleteId, &accessToken.Token, &accessToken.ExpiresAt)
	if err != nil {
		log.Fatal(err)
	}
	return accessToken
}

func getRefreshTokenFromSQL(db *sql.DB, athleteID int) RefreshToken {
	var refreshToken RefreshToken
	query := fmt.Sprintf("SELECT *  FROM strava_refresh_tokens WHERE athlete_id=%d;", athleteID)
	err := db.QueryRow(query).Scan(&refreshToken.AthleteId, &refreshToken.RefreshToken)
	if err != nil {
		log.Fatal(err)
	}
	return refreshToken
}

func updateTokens(db *sql.DB, athleteID int, token AccessTokenResponse) {
	// Prepare the SQL statement
	updateAccessTokenStmt, err := db.Prepare("UPDATE strava_access_tokens SET token=?, expires_at=? WHERE athlete_id=?;")
	if err != nil {
		log.Fatal(err)
	}
	defer updateAccessTokenStmt.Close()

	// Execute the SQL statement with the time value as the parameter
	_, err = updateAccessTokenStmt.Exec(token.AccessToken, time.Unix(token.ExpiresAt, 0), athleteID)
	if err != nil {
		log.Fatal(err)
	}

	// Prepare the SQL statement
	updateRefreshTokenStmt, err := db.Prepare("UPDATE strava_refresh_tokens SET refresh_token=? WHERE athlete_id=?;")
	if err != nil {
		log.Fatal(err)
	}
	defer updateRefreshTokenStmt.Close()

	// Execute the SQL statement with the time value as the parameter
	_, err = updateRefreshTokenStmt.Exec(token.RefreshToken, athleteID)
	if err != nil {
		log.Fatal(err)
	}
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
	ObjectId   int    `json:"object_id"`
	AspectType string `json:"aspect_type"`
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
		fmt.Printf("body: %s", r.Body)
		fmt.Printf("parsing")
		// Parse the JSON data into a struct
		var event WebhookEvent
		err = json.Unmarshal(body, &event)
		if err != nil {
			http.Error(w, "Failed to parse JSON data", http.StatusBadRequest)
			return
		}

		if event.ObjectType == "activity" && event.AspectType == "create" && isTodaySunday() {
			accessToken := getAccessToken()

			workouts, err := fetchWeekWorkouts(w, accessToken)
			prompt := buildPrompt(workouts)
			fmt.Printf("Sending this prompt to chatgpt: %s\n", prompt)

			summary, err := generateSummary(prompt)
			if err != nil {
				fmt.Println("Error:", err)
				return
			}

			fmt.Printf("Summary from chatgpt: %s\n", summary)

			err = updateWorkout(event.ObjectId, summary, "Week Finisher 🔥🔥", accessToken)
			if err != nil {
				fmt.Println("Failed to update workout description:", err)
				return
			}

			fmt.Println("Workout description updated successfully!")

		} else {
			http.Error(w, "Webhook event not supported yet.", http.StatusBadRequest)
			return
		}
	default:
		http.Error(w, "Sorry, only GET and POST are supported", http.StatusNotFound)
	}
}

// isTodaySunday checks if today is Monday.
func isTodaySunday() bool {
	// Get the current day of the week.
	today := time.Now().Weekday()

	// Check if today is Monday.
	return today == time.Sunday
}
