package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
)

const (
	clientID     = "xx"
	clientSecret = "xx"
	redirectURI  = "http://localhost:8080/"
)

type AccessTokenResponse struct {
	AccessToken string `json:"access_token"`
}

type Workout struct {
	ID                 int     `json:"id"`
	Name               string  `json:"name"`
	Distance           float64 `json:"distance"`
	TotalElevationGain float64 `json:"total_elevation_gain"`
	Duration           int     `json:"moving_time"`
}

func main() {
	// Step 1: Redirect the user to the Strava authorization page
	authURL := fmt.Sprintf("https://www.strava.com/oauth/authorize?client_id=%s&redirect_uri=%s/exchange_token&response_type=code&scope=activity:read_all,activity:write&approval_prompt=force", clientID, url.QueryEscape(redirectURI))

	fmt.Println("Please visit the following URL to authorize the application:")
	fmt.Println(authURL)

	// Step 2: After the user authorizes the application, they will be redirected to your redirect URI
	// You need to capture the authorization code from the query parameters of the redirected URL
	http.HandleFunc("/", handler)
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func handler(w http.ResponseWriter, r *http.Request) {
	// Example of capturing the authorization code from a simulated redirect URI
	authorizationCode := r.URL.Query().Get("code")
	fmt.Println("Successfully got an auth code 🎉", authorizationCode)

	// Step 3: Exchange the authorization code for an access token
	accessToken, err := exchangeCodeForToken(authorizationCode)
	if err != nil {
		fmt.Println("Failed to exchange authorization code for access token:", err)
		return
	}
	fmt.Println("Successfully got an accessToken 🎉", accessToken)

	// Step 4: Use the access token to make API requests
	workoutID := 9263490351 // Replace with the ID of the workout you want to fetch
	workout, err := fetchWorkoutDetails(workoutID, accessToken)
	if err != nil {
		fmt.Println("Failed to fetch workout details", err)
		return
	}
	fmt.Println("Successfully fetched the workout 🎉", workout.Name)

	// Print the workout details
	fmt.Println("Workout ID: ", workout.ID)
	fmt.Println("Workout Name: ", workout.Name)
	fmt.Println("Distance: ", workout.Distance)
	fmt.Println("Elevation Gain: ", workout.TotalElevationGain)
	fmt.Println("Duration: ", workout.Duration)

	fmt.Println("Updating workout description..")

	err = updateWorkoutDescription(workoutID, generateDescriptionFromOpenAI(), accessToken)
	if err != nil {
		fmt.Println("Failed to update workout description:", err)
		return
	}
	fmt.Println("Workout description updated successfully!")
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
	params.Add("client_id", clientID)
	params.Add("client_secret", clientSecret)
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

	//prettyPrintJSON(string(body))

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

func updateWorkoutDescription(workoutID int, newDescription string, accessToken string) error {
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

func generateDescriptionFromOpenAI() string {
	return `
Alright, buckle up for some running highlights! 

Your recent adventure was a rollercoaster of effort and triumph. You pushed yourself to the limit, leaving your running history in awe. The weather played its part, adding a sprinkle of excitement to your journey. The route you chose was like a wild maze, keeping you on your toes and surprising you at every turn. Time flew by as you raced against it, clocking in at 1 hour and 25 minutes. You covered a whopping 12.1 kilometers, unleashing your inner gazelle. And let's not forget your pace, a sizzling 5 minutes and 8 seconds per kilometer, making Usain Bolt look twice. 

It was an epic adventure, filled with sweat, determination, and a side of humor. Keep running and conquering those miles!

Your friendly neighbourhood - Stratonova™️ ✌️🏴
`
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
