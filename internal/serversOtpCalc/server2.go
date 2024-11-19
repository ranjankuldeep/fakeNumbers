package serversotpcalc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net/http"
)

// OTPResponse represents the structure of the response from the API
type OTPResponse struct {
	ID       int    `json:"id"`
	Phone    string `json:"phone"`
	Operator string `json:"operator"`
	Product  string `json:"product"`
	Price    int    `json:"price"`
	Status   string `json:"status"`
	Expires  string `json:"expires"`
	SMS      []struct {
		CreatedAt string `json:"created_at"`
		Date      string `json:"date"`
		Sender    string `json:"sender"`
		Text      string `json:"text"`
		Code      string `json:"code"`
	} `json:"sms"`
	CreatedAt string `json:"created_at"`
	Country   string `json:"country"`
}

func GetSMSTextsServer2(otpURL string, id string, headers map[string]string) (string, error) {
	req, err := http.NewRequest("GET", otpURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	if len(headers) > 0 {
		for key, value := range headers {
			req.Header.Add(key, value)
		}
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response body: %w", err)
	}

	var otpResponse OTPResponse
	err = json.Unmarshal(body, &otpResponse)
	if err != nil {
		return "", fmt.Errorf("failed to parse response JSON: %w", err)
	}

	var smsTexts []string
	for _, sms := range otpResponse.SMS {
		smsTexts = append(smsTexts, sms.Text)
	}

	if len(smsTexts) == 0 {
		return "", errors.New("no SMS texts found in the response")
	}
	return smsTexts[0], nil
}
