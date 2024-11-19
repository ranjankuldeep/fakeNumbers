package services

import (
	"fmt"
	"net/http"
	"net/url"
	"time"
)

type OTPDetails struct {
	Email       string
	ServiceName string
	Price       string
	Server      string
	Number      string
	OTP         string
	Ip          string
}
type NumberDetails struct {
	Email       string
	ServiceName string
	ServiceCode string
	Price       string
	Server      string
	Balance     string
	Number      string
	Ip          string
}

// sendMessage sends a message to the Telegram Bot API
func sendMessage(chatID, token, message string) error {
	encodedMessage := url.QueryEscape(message)
	apiURL := fmt.Sprintf(
		"https://api.telegram.org/bot%s/sendMessage?chat_id=%s&text=%s",
		token, chatID, encodedMessage,
	)

	resp, err := http.Get(apiURL)
	if err != nil {
		return fmt.Errorf("failed to send message: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP error! status: %d", resp.StatusCode)
	}

	return nil
}

// NumberGetDetails sends number get details to Telegram
func NumberGetDetails(numberInfo NumberDetails) error {
	result := "Number Get\n\n"
	result += fmt.Sprintf("Date => %s\n\n", time.Now().Format("02-01-2006 03:04:05PM"))
	result += fmt.Sprintf("User Email => %s\n\n", numberInfo.Email)
	result += fmt.Sprintf("Service Name => %s\n\n", numberInfo.ServiceName)
	result += fmt.Sprintf("Service Code => %s\n\n", numberInfo.ServiceCode)
	result += fmt.Sprintf("Price => %s₹\n\n", numberInfo.Price)
	result += fmt.Sprintf("Server => %s\n\n", numberInfo.Server)
	result += fmt.Sprintf("Number => %d\n\n", numberInfo.Number)
	result += fmt.Sprintf("Balance => %s₹\n\n", numberInfo.Balance)
	result += fmt.Sprintf("IP Details => %s\n\n", numberInfo.Ip)

	// Send the message via Telegram Bot API
	err := sendMessage("6769991787", "7032433639:AAGGbZb_HgGBGHwqOpnw2Bv6rriYyOAjzJ8", result)
	if err != nil {
		return err
	}

	return nil
}

// OtpGetDetails sends OTP get details to Telegram
func OtpGetDetails(otpInfo OTPDetails) error {
	result := "Otp Get\n\n"
	result += fmt.Sprintf("Date => %s\n\n", time.Now().Format("02-01-2006 03:04:05PM"))
	result += fmt.Sprintf("User Email => %s\n\n", otpInfo.Email)
	result += fmt.Sprintf("Service Name => %s\n\n", otpInfo.ServiceName)
	result += fmt.Sprintf("Price => %s₹\n\n", otpInfo.Price)
	result += fmt.Sprintf("Server => %s\n\n", otpInfo.Server)
	result += fmt.Sprintf("Number => %s\n\n", otpInfo.Number)
	result += fmt.Sprintf("Otp => %s\n\n", otpInfo.OTP)
	result += fmt.Sprintf("IP Details => %s\n\n", otpInfo.Ip)

	// Send the message via Telegram Bot API
	err := sendMessage("6769991787", "7032433639:AAGGbZb_HgGBGHwqOpnw2Bv6rriYyOAjzJ8", result)
	if err != nil {
		return err
	}

	return nil
}

// NumberCancelDetails sends number cancel details to Telegram
func NumberCancelDetails(email, serviceName, price, server string, number int64, balance, ip string) error {
	result := "Number Cancel\n\n"
	result += fmt.Sprintf("Date => %s\n\n", time.Now().Format("02-01-2006 03:04:05PM"))
	result += fmt.Sprintf("User Email => %s\n\n", email)
	result += fmt.Sprintf("Service Name => %s\n\n", serviceName)
	result += fmt.Sprintf("Price => %s₹\n\n", price)
	result += fmt.Sprintf("Server => %s\n\n", server)
	result += fmt.Sprintf("Number => %d\n\n", number)
	result += fmt.Sprintf("Balance => %s₹\n\n", balance)
	result += "Status => Number Cancelled\n\n"
	result += fmt.Sprintf("IP Details => %s\n\n", ip)

	// Send the message via Telegram Bot API
	err := sendMessage("6769991787", "7032433639:AAGGbZb_HgGBGHwqOpnw2Bv6rriYyOAjzJ8", result)
	if err != nil {
		return err
	}

	return nil
}
