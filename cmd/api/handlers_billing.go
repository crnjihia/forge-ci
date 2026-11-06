// path: cmd/api/handlers_billing.go
package main

import (
	"encoding/json"
	"io"
	"net/http"

	"github.com/crnji/forge-ci/internal/billing"
	"github.com/go-chi/render"
)

type TopUpRequest struct {
	PhoneNumber string  `json:"phone_number"`
	AmountKES   float64 `json:"amount_kes"`
	UserID      string  `json:"user_id"`
}

func handleMpesaTopUp(billingService *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if billingService == nil {
			render.Status(r, http.StatusServiceUnavailable)
			render.JSON(w, r, map[string]string{"error": "billing service is not configured"})
			return
		}

		var req TopUpRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "invalid JSON body"})
			return
		}

		if req.PhoneNumber == "" {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "phone_number is required"})
			return
		}

		if req.AmountKES <= 0 {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "amount_kes must be greater than zero"})
			return
		}

		if req.UserID == "" {
			req.UserID = "default_user"
		}

		resp, err := billingService.InitiateTopup(r.Context(), req.UserID, req.PhoneNumber, req.AmountKES)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}

		render.JSON(w, r, map[string]interface{}{
			"status":              "initiated",
			"checkout_request_id": resp.CheckoutRequestID,
			"merchant_request_id": resp.MerchantRequestID,
			"customer_message":    resp.CustomerMessage,
			"amount_kes":          req.AmountKES,
		})
	}
}

func handleMpesaCallback(billingService *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if billingService == nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"ResultCode":0,"ResultDesc":"Accepted"}`))
			return
		}

		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, map[string]string{"error": "unable to read callback body"})
			return
		}

		_, _ = billingService.ProcessCallback(r.Context(), bodyBytes)

		// Safaricom Daraja expects standard acknowledgment format
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ResultCode":0,"ResultDesc":"Accepted"}`))
	}
}

func handleGetBillingBalance(billingService *billing.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if billingService == nil {
			render.JSON(w, r, map[string]interface{}{
				"user_id":     "default_user",
				"balance_kes": 1000.00,
				"currency":    "KES",
			})
			return
		}

		userID := r.URL.Query().Get("user_id")
		if userID == "" {
			userID = "default_user"
		}

		balance, err := billingService.GetBalance(r.Context(), userID)
		if err != nil {
			render.Status(r, http.StatusInternalServerError)
			render.JSON(w, r, map[string]string{"error": err.Error()})
			return
		}

		render.JSON(w, r, map[string]interface{}{
			"user_id":     userID,
			"balance_kes": balance,
			"currency":    "KES",
		})
	}
}
