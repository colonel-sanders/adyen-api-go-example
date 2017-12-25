/*
	export ADYEN_CLIENT_TOKEN="YOUR_ADYEN_ENCRYPTED_URL"
	export ADYEN_USERNAME="YOUR_ADYEN_API_USERNAME"
	export ADYEN_PASSWORD="YOUR_API_PASSWORD"
	export ADYEN_ACCOUNT="YOUR_MERCHANT_ACCOUNT"

	# API settings for Adyen Hosted Payment pages
	export ADYEN_HMAC="YOUR_HMAC_KEY"
	export ADYEN_SKINCODE="YOUR_SKIN_CODE"
	export ADYEN_SHOPPER_LOCALE="en_GB"
*/

package main

import (
	"encoding/json"
	"fmt"
	"github.com/gorilla/mux"
	"github.com/zhutik/adyen-api-go"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"
)

// TemplateConfig for HTML template
type TemplateConfig struct {
	EncURL string
	Time   string
}

var Logger *log.Logger

func randInt(min int, max int) int {
	return min + rand.Intn(max-min)
}

func randomString(l int) string {
	bytes := make([]byte, l)
	for i := 0; i < l; i++ {
		bytes[i] = byte(randInt(65, 90))
	}
	return string(bytes)
}

// initAdyen init Adyen API instance
func initAdyen() *adyen.Adyen {
	instance := adyen.New(
		adyen.Testing,
		os.Getenv("ADYEN_USERNAME"),
		os.Getenv("ADYEN_PASSWORD"),
		os.Getenv("ADYEN_CLIENT_TOKEN"),
		os.Getenv("ADYEN_ACCOUNT"),
	)

	Logger = log.New(os.Stdout, "Adyen Playground: ", log.Ldate|log.Ltime|log.Lshortfile)

	instance.SetCurrency("EUR")
	instance.AttachLogger(Logger)

	return instance
}

func initAdyenHPP() *adyen.Adyen {
	instance := adyen.NewWithHPP(
		adyen.Testing,
		os.Getenv("ADYEN_USERNAME"),
		os.Getenv("ADYEN_PASSWORD"),
		os.Getenv("ADYEN_CLIENT_TOKEN"),
		os.Getenv("ADYEN_ACCOUNT"),
		os.Getenv("ADYEN_HMAC"),
		os.Getenv("ADYEN_SKINCODE"),
		os.Getenv("ADYEN_SHOPPER_LOCALE"),
	)

	Logger = log.New(os.Stdout, "Adyen Playground: ", log.Ldate|log.Ltime|log.Lshortfile)

	instance.SetCurrency("EUR")
	instance.AttachLogger(Logger)

	return instance
}

/**
 * Show Adyen Payment form
 */
func showForm(w http.ResponseWriter, r *http.Request) {
	instance := adyen.New(
		adyen.Testing,
		os.Getenv("ADYEN_USERNAME"),
		os.Getenv("ADYEN_PASSWORD"),
		os.Getenv("ADYEN_CLIENT_TOKEN"),
		os.Getenv("ADYEN_ACCOUNT"),
	)
	now := time.Now()
	cwd, _ := os.Getwd()

	config := TemplateConfig{
		EncURL: instance.ClientURL(),
		Time:   now.Format(time.RFC3339),
	}

	t := template.Must(template.ParseGlob(filepath.Join(cwd, "./templates/*")))
	err := t.ExecuteTemplate(w, "indexPage", config)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

/**
 * Handle post request and perform payment authorization
 */
func performPayment(w http.ResponseWriter, r *http.Request) {
	instance := initAdyen()

	r.ParseForm()

	rand.Seed(time.Now().UTC().UnixNano())

	var g *adyen.AuthoriseResponse
	var err error

	amount, err := strconv.ParseFloat(r.Form.Get("amount"), 32)

	if err != nil {
		http.Error(w, "Failed! Can not convert amount to float", http.StatusInternalServerError)
		return
	}

	reference := r.Form.Get("reference")

	// multiple value by 100, as specified in Adyen documentation
	adyenAmount := float32(amount) * 100

	// Form was submitted with encrypted data
	if len(r.Form.Get("adyen-encrypted-data")) > 0 {
		req := &adyen.AuthoriseEncrypted{
			Amount:           &adyen.Amount{Value: adyenAmount, Currency: instance.Currency},
			MerchantAccount:  os.Getenv("ADYEN_ACCOUNT"),
			AdditionalData:   &adyen.AdditionalData{Content: r.Form.Get("adyen-encrypted-data")},
			ShopperReference: r.Form.Get("shopperReference"),
			Reference:        reference, // order number or some business reference
		}

		if len(r.Form.Get("is_recurring")) > 0 {
			req.Recurring = &adyen.Recurring{Contract:adyen.RecurringPaymentRecurring}
		}

		g, err = instance.Payment().AuthoriseEncrypted(req)
	} else {
		req := &adyen.Authorise{
			Card: &adyen.Card{
				Number:       r.Form.Get("number"),
				ExpireMonth:  r.Form.Get("expiryMonth"),
				ExpireYear:   r.Form.Get("expiryYear"),
				HolderName:   r.Form.Get("holderName"),
				Cvc:          r.Form.Get("cvc"),
			},
			Amount:           &adyen.Amount{Value: adyenAmount, Currency: instance.Currency},
			MerchantAccount:  os.Getenv("ADYEN_ACCOUNT"),
			Reference:        reference, // order number or some business reference
			ShopperReference: r.Form.Get("shopperReference"),
		}

		g, err = instance.Payment().Authorise(req)
	}

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response, err := json.Marshal(g)

	w.Header().Set("Content-Type", "application/json")
	w.Write(response)
}

func performCapture(w http.ResponseWriter, r *http.Request) {
	instance := initAdyen()

	r.ParseForm()

	amount, err := strconv.ParseFloat(r.Form.Get("amount"), 32)

	if err != nil {
		http.Error(w, "Failed! Can not convert amount to float", http.StatusInternalServerError)
		return
	}

	req := &adyen.Capture{
		ModificationAmount: &adyen.Amount{Value: float32(amount), Currency: instance.Currency},
		MerchantAccount:    os.Getenv("ADYEN_ACCOUNT"),       // Merchant Account setting
		Reference:          r.Form.Get("reference"),          // order number or some business reference
		OriginalReference:  r.Form.Get("original-reference"), // PSP reference that came as authorization results
	}

	g, err := instance.Modification().Capture(req)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response, err := json.Marshal(g)

	w.Header().Set("Content-Type", "application/json")
	w.Write(response)
}

func performCancel(w http.ResponseWriter, r *http.Request) {
	instance := initAdyen()

	r.ParseForm()

	req := &adyen.Cancel{
		Reference:         r.Form.Get("reference"),          // order number or some business reference
		MerchantAccount:   os.Getenv("ADYEN_ACCOUNT"),       // Merchant Account setting
		OriginalReference: r.Form.Get("original-reference"), // PSP reference that came as authorization result
	}

	g, err := instance.Modification().Cancel(req)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response, err := json.Marshal(g)

	w.Header().Set("Content-Type", "application/json")
	w.Write(response)
}

func performRefund(w http.ResponseWriter, r *http.Request) {
	instance := initAdyen()

	r.ParseForm()

	amount, err := strconv.ParseFloat(r.Form.Get("amount"), 32)

	if err != nil {
		http.Error(w, "Failed! Can not convert amount to float", http.StatusInternalServerError)
		return
	}

	req := &adyen.Refund{
		ModificationAmount: &adyen.Amount{Value: float32(amount), Currency: instance.Currency},
		Reference:          r.Form.Get("reference"),          // order number or some business reference
		MerchantAccount:    os.Getenv("ADYEN_ACCOUNT"),       // Merchant Account setting
		OriginalReference:  r.Form.Get("original-reference"), // PSP reference that came as authorization result
	}

	g, err := instance.Modification().Refund(req)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	response, err := json.Marshal(g)

	w.Header().Set("Content-Type", "application/json")
	w.Write(response)
}

func performDirectoryLookup(w http.ResponseWriter, r *http.Request) {
	instance := initAdyenHPP()

	timeIn := time.Now().Local().Add(time.Minute * time.Duration(60))

	req := &adyen.DirectoryLookupRequest{
		CurrencyCode:      "EUR",
		MerchantAccount:   os.Getenv("ADYEN_ACCOUNT"),
		PaymentAmount:     1000,
		SkinCode:          os.Getenv("ADYEN_SKINCODE"),
		MerchantReference: "DE-100" + randomString(6),
		SessionsValidity:  timeIn.Format(time.RFC3339),
		CountryCode:       "NL",
	}

	g, err := instance.Payment().DirectoryLookup(req)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	cwd, _ := os.Getwd()
	t := template.Must(template.ParseGlob(filepath.Join(cwd, "./templates/*")))
	err = t.ExecuteTemplate(w, "hpp_payment_methods", g)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func performHpp(w http.ResponseWriter, r *http.Request) {
	instance := initAdyenHPP()

	timeIn := time.Now().Local().Add(time.Minute * time.Duration(60))

	// 5 days
	shipTime := time.Now().Local().Add(time.Hour * 24 * time.Duration(5))

	req := &adyen.SkipHppRequest{
		MerchantReference: "DE-100" + randomString(6),
		PaymentAmount:     1000,
		CurrencyCode:      instance.Currency,
		ShipBeforeDate:    shipTime.Format(time.RFC3339),
		SkinCode:          os.Getenv("ADYEN_SKINCODE"),
		MerchantAccount:   os.Getenv("ADYEN_ACCOUNT"),
		ShopperLocale:     "en_GB",
		SessionsValidity:  timeIn.Format(time.RFC3339),
		CountryCode:       "NL",
		BrandCode:         "ideal",
		IssuerID:          "1121",
	}

	url, err := instance.Payment().GetHPPRedirectURL(req)

	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	http.Redirect(w, r, url, http.StatusTemporaryRedirect)
}

func main() {
	fmt.Println("Checking environment variables...")

	if len(os.Getenv("ADYEN_USERNAME")) == 0 ||
		len(os.Getenv("ADYEN_PASSWORD")) == 0 ||
		len(os.Getenv("ADYEN_CLIENT_TOKEN")) == 0 ||
		len(os.Getenv("ADYEN_ACCOUNT")) == 0 {
		panic("Some of the required varibles are missing or empty.\nPlease make sure\nADYEN_USERNAME\nADYEN_PASSWORD\nADYEN_CLIENT_TOKEN\nADYEN_ACCOUNT\nare set as environment variables")
	}

	port := 8080

	if len(os.Getenv("APPLICATION_PORT")) != 0 {
		port, _ = strconv.Atoi(os.Getenv("APPLICATION_PORT"))
	}

	fmt.Println(fmt.Sprintf("Start listening connections on port %d...", port))

	cwd, err := os.Getwd()
	if err != nil {
		panic("Can't read current working directory")
	}

	r := mux.NewRouter()

	r.HandleFunc("/", showForm)
	r.HandleFunc("/perform_payment", performPayment)
	r.HandleFunc("/perform_capture", performCapture)
	r.HandleFunc("/perform_cancel", performCancel)
	r.HandleFunc("/perform_lookup", performDirectoryLookup)
	r.HandleFunc("/perform_hpp", performHpp)
	r.HandleFunc("/perform_refund", performRefund)
	s := http.StripPrefix("/static/", http.FileServer(http.Dir(cwd+"/static/")))
	r.PathPrefix("/static/").Handler(s)

	http.Handle("/", r)

	http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}
