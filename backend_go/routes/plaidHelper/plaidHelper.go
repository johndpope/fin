package plaidHelper

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fin-go/types"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"encoding/json"

	"github.com/jmoiron/sqlx"
	"github.com/joho/godotenv"
	"github.com/plaid/plaid-go/plaid"
)

var (
	PLAID_CLIENT_ID                      = ""
	PLAID_SECRET                         = ""
	PLAID_ENV                            = ""
	PLAID_PRODUCTS                       = ""
	PLAID_COUNTRY_CODES                  = ""
	PLAID_REDIRECT_URI                   = ""
	APP_PORT                             = ""
	client              *plaid.APIClient = nil
)

var environments = map[string]plaid.Environment{
	"sandbox":     plaid.Sandbox,
	"development": plaid.Development,
	"production":  plaid.Production,
}

func init() {
	// load env vars from .env file
	err := godotenv.Load()
	if err != nil {
		fmt.Println("Error when loading environment variables from .env file %w", err)
	}

	// set constants from env
	PLAID_CLIENT_ID = os.Getenv("PLAID_CLIENT_ID")
	PLAID_SECRET = os.Getenv("PLAID_SECRET")

	if PLAID_CLIENT_ID == "" || PLAID_SECRET == "" {
		log.Fatal("Error: PLAID_SECRET or PLAID_CLIENT_ID is not set. Did you copy .env.example to .env and fill it out?")
	}

	PLAID_ENV = os.Getenv("PLAID_ENV")
	PLAID_PRODUCTS = os.Getenv("PLAID_PRODUCTS")
	PLAID_COUNTRY_CODES = os.Getenv("PLAID_COUNTRY_CODES")
	PLAID_REDIRECT_URI = os.Getenv("PLAID_REDIRECT_URI")
	APP_PORT = os.Getenv("APP_PORT")

	// set defaults
	if PLAID_PRODUCTS == "" {
		PLAID_PRODUCTS = "transactions"
	}
	if PLAID_COUNTRY_CODES == "" {
		PLAID_COUNTRY_CODES = "US"
	}
	if PLAID_ENV == "" {
		PLAID_ENV = "sandbox"
	}
	if APP_PORT == "" {
		APP_PORT = "8000"
	}
	if PLAID_CLIENT_ID == "" {
		log.Fatal("PLAID_CLIENT_ID is not set. Make sure to fill out the .env file")
	}
	if PLAID_SECRET == "" {
		log.Fatal("PLAID_SECRET is not set. Make sure to fill out the .env file")
	}

	// create Plaid client
	configuration := plaid.NewConfiguration()
	configuration.AddDefaultHeader("PLAID-CLIENT-ID", PLAID_CLIENT_ID)
	configuration.AddDefaultHeader("PLAID-SECRET", PLAID_SECRET)
	configuration.UseEnvironment(environments[PLAID_ENV])
	client = plaid.NewAPIClient(configuration)
}

/*
func main() {
	r := gin.Default()

	r.POST("/api/info", info)

	// For OAuth flows, the process looks as follows.
	// 1. Create a link token with the redirectURI (as white listed at https://dashboard.plaid.com/team/api).
	// 2. Once the flow succeeds, Plaid Link will redirect to redirectURI with
	// additional parameters (as required by OAuth standards and Plaid).
	// 3. Re-initialize with the link token (from step 1) and the full received redirect URI
	// from step 2.

	r.POST("/api/set_access_token", getAccessToken)
	r.POST("/api/create_link_token_for_payment", createLinkTokenForPayment)
	r.GET("/api/auth", auth)
	r.GET("/api/accounts", accounts)
	r.GET("/api/balance", balance)
	r.GET("/api/item", item)
	r.POST("/api/item", item)
	r.GET("/api/identity", identity)
	r.GET("/api/transactions", transactions)
	r.POST("/api/transactions", transactions)
	r.GET("/api/payment", payment)
	r.GET("/api/create_public_token", createPublicToken)
	r.POST("/api/create_link_token", createLinkToken)
	r.GET("/api/investment_transactions", investmentTransactions)
	r.GET("/api/holdings", holdings)
	r.GET("/api/assets", assets)

	err := r.Run(":" + APP_PORT)
	if err != nil {
		renderError("unable to start server")
	}
}*/

// We store the access_token in memory - in production, store it in a secure
// persistent data store.
var accessToken string
var itemID string

var paymentID string

func renderError(res http.ResponseWriter, originalErr error) {
	if plaidError, err := plaid.ToPlaidError(originalErr); err == nil {
		// Return 200 and allow the front end to render the error.
		// c.JSON(http.StatusOK, gin.H{"error": plaidError})
		errJson := json.NewEncoder(res).Encode(plaidError)
		errString := fmt.Sprintf("Error with Create Link Token: %v \n", errJson)
		log.Println(errString)
		res.WriteHeader(http.StatusInternalServerError)
		res.Write([]byte(errString))
		return
	}
	res.WriteHeader(http.StatusInternalServerError)
	data := make(map[string]string)
	data["error"] = originalErr.Error()
	errString := fmt.Sprintf("%v", data)
	res.Write([]byte(errString))

}

func GetAccessToken() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		publicToken := req.FormValue("public_token")
		fmt.Println("GetAccessToken!")
		fmt.Println(req)
		// exchange the public_token for an access_token
		exchangePublicTokenResp, _, err := client.PlaidApi.ItemPublicTokenExchange(ctx).ItemPublicTokenExchangeRequest(
			*plaid.NewItemPublicTokenExchangeRequest(publicToken),
		).Execute()
		if err != nil {
			renderError(res, err)
			return
		}

		accessToken = exchangePublicTokenResp.GetAccessToken()
		itemID = exchangePublicTokenResp.GetItemId()

		fmt.Println("public token: " + publicToken)
		fmt.Println("access token: " + accessToken)
		fmt.Println("item ID: " + itemID)

		res.WriteHeader(http.StatusOK)

		data := make(map[string]string)
		data["access_token"] = accessToken
		data["item_id"] = itemID

		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}

	}

}

// This functionality is only relevant for the UK Payment Initiation product.
// Creates a link token configured for payment initiation. The payment
// information will be associated with the link token, and will not have to be
// passed in again when we initialize Plaid Link.
func CreateLinkTokenForPayment() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		// Create payment recipient
		paymentRecipientRequest := plaid.NewPaymentInitiationRecipientCreateRequest("Harry Potter")
		paymentRecipientRequest.SetIban("GB33BUKB20201555555555")
		paymentRecipientRequest.SetAddress(*plaid.NewPaymentInitiationAddress(
			[]string{"4 Privet Drive"},
			"Little Whinging",
			"11111",
			"GB",
		))
		paymentRecipientCreateResp, _, err := client.PlaidApi.PaymentInitiationRecipientCreate(ctx).PaymentInitiationRecipientCreateRequest(*paymentRecipientRequest).Execute()
		if err != nil {
			renderError(res, err)
			return
		}

		// Create payment
		paymentCreateRequest := plaid.NewPaymentInitiationPaymentCreateRequest(
			paymentRecipientCreateResp.GetRecipientId(),
			"paymentRef",
			*plaid.NewPaymentAmount("GBP", 12.34),
		)
		paymentCreateResp, _, err := client.PlaidApi.PaymentInitiationPaymentCreate(ctx).PaymentInitiationPaymentCreateRequest(*paymentCreateRequest).Execute()
		if err != nil {
			renderError(res, err)
			return
		}

		paymentID = paymentCreateResp.GetPaymentId()
		fmt.Println("payment id: " + paymentID)

		linkTokenCreateReqPaymentInitiation := plaid.NewLinkTokenCreateRequestPaymentInitiation(paymentID)
		linkToken, err := linkTokenCreate(ctx, linkTokenCreateReqPaymentInitiation)

		data := make(map[string]string)
		data["link_token"] = linkToken
		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}
	}

}

func Auth() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		authGetResp, _, err := client.PlaidApi.AuthGet(ctx).AuthGetRequest(
			*plaid.NewAuthGetRequest(accessToken),
		).Execute()

		if err != nil {
			renderError(res, err)
			return
		}

		var data map[string]interface{}
		data["accounts"] = authGetResp.GetAccounts()
		data["numbers"] = authGetResp.GetNumbers()
		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}

	}

}

func Accounts() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()

		accountsGetResp, _, err := client.PlaidApi.AccountsGet(ctx).AccountsGetRequest(
			*plaid.NewAccountsGetRequest(accessToken),
		).Execute()

		if err != nil {
			renderError(res, err)
			return
		}

		res.WriteHeader(http.StatusOK)
		var data map[string]interface{}
		data["accounts"] = accountsGetResp.GetAccounts()
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}
	}
}

func Balance() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()

		balancesGetResp, _, err := client.PlaidApi.AccountsBalanceGet(ctx).AccountsBalanceGetRequest(
			*plaid.NewAccountsBalanceGetRequest(accessToken),
		).Execute()

		if err != nil {
			renderError(res, err)
			return
		}

		res.WriteHeader(http.StatusOK)
		var data map[string]interface{}
		data["accounts"] = balancesGetResp.GetAccounts()
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}
	}
}

func Item() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {

		ctx := req.Context()
		itemGetResp, _, err := client.PlaidApi.ItemGet(ctx).ItemGetRequest(
			*plaid.NewItemGetRequest(accessToken),
		).Execute()

		if err != nil {
			renderError(res, err)
			return
		}

		institutionGetByIdResp, _, err := client.PlaidApi.InstitutionsGetById(ctx).InstitutionsGetByIdRequest(
			*plaid.NewInstitutionsGetByIdRequest(
				*itemGetResp.GetItem().InstitutionId.Get(),
				convertCountryCodes(strings.Split(PLAID_COUNTRY_CODES, ",")),
			),
		).Execute()

		var data map[string]interface{}
		data["item"] = itemGetResp.GetItem()
		data["institution"] = institutionGetByIdResp.GetInstitution()
		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}

	}
}

func Identity() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		identityGetResp, _, err := client.PlaidApi.IdentityGet(ctx).IdentityGetRequest(
			*plaid.NewIdentityGetRequest(accessToken),
		).Execute()

		if err != nil {
			renderError(res, err)
			return
		}

		var data map[string]interface{}
		data["identity"] = identityGetResp.GetAccounts()
		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}
	}

}

func Transactions() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		// pull transactions for the past 30 days
		endDate := time.Now().Local().Format("2006-01-02")
		startDate := time.Now().Local().Add(-30 * 24 * time.Hour).Format("2006-01-02")

		transactionsResp, _, err := client.PlaidApi.TransactionsGet(ctx).TransactionsGetRequest(
			*plaid.NewTransactionsGetRequest(
				accessToken,
				startDate,
				endDate,
			),
		).Execute()

		if err != nil {
			renderError(res, err)
			return
		}

		var data map[string]interface{}
		data["accounts"] = transactionsResp.GetAccounts()
		data["transactions"] = transactionsResp.GetTransactions()

		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}

	}
}

// This functionality is only relevant for the UK Payment Initiation product.
// Retrieve Payment for a specified Payment ID
func Payment() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		paymentGetResp, _, err := client.PlaidApi.PaymentInitiationPaymentGet(ctx).PaymentInitiationPaymentGetRequest(
			*plaid.NewPaymentInitiationPaymentGetRequest(paymentID),
		).Execute()

		if err != nil {
			renderError(res, err)
			return
		}

		var data map[string]interface{}
		data["payment"] = paymentGetResp

		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}
	}
}

func InvestmentTransactions() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		endDate := time.Now().Local().Format("2006-01-02")
		startDate := time.Now().Local().Add(-30 * 24 * time.Hour).Format("2006-01-02")

		request := plaid.NewInvestmentsTransactionsGetRequest(accessToken, startDate, endDate)
		invTxResp, _, err := client.PlaidApi.InvestmentsTransactionsGet(ctx).InvestmentsTransactionsGetRequest(*request).Execute()

		if err != nil {
			renderError(res, err)
			return
		}

		var data map[string]interface{}
		data["investment_transactions"] = invTxResp

		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}
	}

}

func Holdings() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		holdingsGetResp, _, err := client.PlaidApi.InvestmentsHoldingsGet(ctx).InvestmentsHoldingsGetRequest(
			*plaid.NewInvestmentsHoldingsGetRequest(accessToken),
		).Execute()
		if err != nil {
			renderError(res, err)
			return
		}

		var data map[string]interface{}
		data["holdings"] = holdingsGetResp
		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}
	}
}

/*
func info(context *gin.Context) {
	context.JSON(http.StatusOK, map[string]interface{}{
		"item_id":      itemID,
		"access_token": accessToken,
		"products":     strings.Split(PLAID_PRODUCTS, ","),
	})
}
*/
func CreatePublicToken() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {
		ctx := req.Context()

		// Create a one-time use public_token for the Item.
		// This public_token can be used to initialize Link in update mode for a user
		publicTokenCreateResp, _, err := client.PlaidApi.ItemCreatePublicToken(ctx).ItemPublicTokenCreateRequest(
			*plaid.NewItemPublicTokenCreateRequest(accessToken),
		).Execute()

		if err != nil {
			renderError(res, err)
			return
		}

		var data map[string]interface{}
		data["public_token"] = publicTokenCreateResp.GetPublicToken()
		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}
	}

}

func CreateLinkToken() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		/*linkToken, err := linkTokenCreate(ctx, nil)
		if err != nil {
			renderError(res, err)
			return
		}
		var data map[string]interface{}
		data["link_token"] = linkToken
		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}*/

		user := plaid.LinkTokenCreateRequestUser{
			ClientUserId: "00001",
		}
		request := plaid.NewLinkTokenCreateRequest(
			"Plaid Test",
			"en",
			[]plaid.CountryCode{plaid.COUNTRYCODE_US},
			user,
		)
		request.SetProducts([]plaid.Products{plaid.PRODUCTS_AUTH})
		request.SetLinkCustomizationName("default")
		request.SetWebhook("http://0.0.0.0:9028")
		request.SetAccountFilters(plaid.LinkTokenAccountFilters{
			Depository: &plaid.DepositoryFilter{
				AccountSubtypes: []plaid.AccountSubtype{plaid.ACCOUNTSUBTYPE_CHECKING, plaid.ACCOUNTSUBTYPE_SAVINGS},
			},
		})
		resp, _, err := client.PlaidApi.LinkTokenCreate(ctx).LinkTokenCreateRequest(*request).Execute()
		if err != nil {
			renderError(res, err)
			return
		}
		linkToken := resp.GetLinkToken()
		data := make(map[string]string)
		data["link_token"] = linkToken
		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}
	}
}

func convertCountryCodes(countryCodeStrs []string) []plaid.CountryCode {
	countryCodes := []plaid.CountryCode{}

	for _, countryCodeStr := range countryCodeStrs {
		countryCodes = append(countryCodes, plaid.CountryCode(countryCodeStr))
	}

	return countryCodes
}

func convertProducts(productStrs []string) []plaid.Products {
	products := []plaid.Products{}

	for _, productStr := range productStrs {
		products = append(products, plaid.Products(productStr))
	}

	return products
}

// linkTokenCreate creates a link token using the specified parameters
func linkTokenCreate(ctx context.Context,
	paymentInitiation *plaid.LinkTokenCreateRequestPaymentInitiation,
) (string, error) {

	countryCodes := convertCountryCodes(strings.Split(PLAID_COUNTRY_CODES, ","))
	products := convertProducts(strings.Split(PLAID_PRODUCTS, ","))
	redirectURI := PLAID_REDIRECT_URI

	user := plaid.LinkTokenCreateRequestUser{
		ClientUserId: time.Now().String(),
	}

	request := plaid.NewLinkTokenCreateRequest(
		"Plaid Quickstart",
		"en",
		countryCodes,
		user,
	)

	request.SetProducts(products)

	if redirectURI != "" {
		request.SetRedirectUri(redirectURI)
	}

	if paymentInitiation != nil {
		request.SetPaymentInitiation(*paymentInitiation)
	}

	linkTokenCreateResp, _, err := client.PlaidApi.LinkTokenCreate(ctx).LinkTokenCreateRequest(*request).Execute()

	if err != nil {
		return "", err
	}

	return linkTokenCreateResp.GetLinkToken(), nil
}

func Assets() func(w http.ResponseWriter, r *http.Request) {

	return func(res http.ResponseWriter, req *http.Request) {
		ctx := req.Context()
		// create the asset report
		assetReportCreateResp, _, err := client.PlaidApi.AssetReportCreate(ctx).AssetReportCreateRequest(
			*plaid.NewAssetReportCreateRequest([]string{accessToken}, 10),
		).Execute()
		if err != nil {
			renderError(res, err)
			return
		}

		assetReportToken := assetReportCreateResp.GetAssetReportToken()

		// get the asset report
		assetReportGetResp, err := pollForAssetReport(ctx, client, assetReportToken)
		if err != nil {
			renderError(res, err)
			return
		}

		// get it as a pdf
		pdfRequest := plaid.NewAssetReportPDFGetRequest(assetReportToken)
		pdfFile, _, err := client.PlaidApi.AssetReportPdfGet(ctx).AssetReportPDFGetRequest(*pdfRequest).Execute()
		if err != nil {
			renderError(res, err)
			return
		}

		reader := bufio.NewReader(pdfFile)
		content, err := ioutil.ReadAll(reader)
		if err != nil {
			renderError(res, err)
			return
		}

		// convert pdf to base64
		encodedPdf := base64.StdEncoding.EncodeToString(content)

		var data map[string]interface{}
		data["pdf"] = encodedPdf
		data["json"] = assetReportGetResp.GetReport()
		res.WriteHeader(http.StatusOK)
		if err := json.NewEncoder(res).Encode(data); err != nil {
			renderError(res, err)
		}
	}
}

func pollForAssetReport(ctx context.Context, client *plaid.APIClient, assetReportToken string) (*plaid.AssetReportGetResponse, error) {
	numRetries := 20
	request := plaid.NewAssetReportGetRequest(assetReportToken)

	for i := 0; i < numRetries; i++ {
		response, _, err := client.PlaidApi.AssetReportGet(ctx).AssetReportGetRequest(*request).Execute()
		if err != nil {
			plaidErr, err := plaid.ToPlaidError(err)
			if plaidErr.ErrorCode == "PRODUCT_NOT_READY" {
				time.Sleep(1 * time.Second)
				continue
			} else {
				return nil, err
			}
		} else {
			return &response, nil
		}
	}
	return nil, errors.New("Timed out when polling for an asset report.")
}

/*
func CreateFromPublicTokenFunction() func(http.ResponseWriter, *http.Request) {
	return func(res http.ResponseWriter, req *http.Request) {

		var err error

		decoder := json.NewDecoder(req.Body)
		var item types.CreateTokenPost
		err = decoder.Decode(&item)
		if err != nil {
			// panic(err)
			errString := fmt.Sprintf("Error with Create Token Post Request: %v \n", err)
			log.Println(errString)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(errString))
		}

		pClient := client

		createLinkTokenResp, err := CreateLinkToken
		(LinkTokenConfigs{
			User: &LinkTokenUser{
				ClientUserID:             time.Now().String(),
				LegalName:                "Fin User",
				PhoneNumber:              "8008675309",
				EmailAddress:             "test@email.com",
				PhoneNumberVerifiedTime:  time.Now(),
				EmailAddressVerifiedTime: time.Now(),
			},
			ClientName:   "Plaid Test",
			Products:     []string{"auth"},
			CountryCodes: []string{"US"},
			Webhook:      "https://webhook-uri.com",
			AccountFilters: &map[string]map[string][]string{
				"depository": {
					"account_subtypes": {"all"},
				},
			},
			Language:              "en",
			LinkCustomizationName: "default",
		})
		if err != nil {
			// panic(err)
			errString := fmt.Sprintf("Error with Create Link Token: %v \n", err)
			log.Println(errString)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(errString))
		}

		pRes, err := pClient.ExchangePublicToken(CreateLinkToken
			Resp.LinkToken)
		if err != nil {
			// panic(err)
			errString := fmt.Sprintf("Error with Exchange Public Token: %v \n", err)
			log.Println(errString)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(errString))
		}

		// // Changing to Link token flow (public key deprecated)
		// pRes, err := pClient.ExchangePublicToken(item.Token)
		// if err != nil {
		// 	// panic(err)
		// 	errString := fmt.Sprintf("Error with Create Token Client Exchange Token request: %v \n", err)
		// 	log.Println(errString)
		// 	res.WriteHeader(http.StatusInternalServerError)
		// 	res.Write([]byte(errString))
		// }

		txn := db.DBCon.MustBegin()

		iTok := types.ItemToken{}
		iTok.ItemID = pRes.ItemID
		iTok.AccessToken = pRes.AccessToken
		iTok.Institution = item.Name
		iTok.Provider = "Plaid"
		iTok.NeedsReLogin = false
		istmt := types.PrepItemSt(txn)
		upsertItemToken(iTok, istmt)
		astmt := types.PrepAccountSt(txn)

		pAccountRes, err := pClient.GetAccounts(pRes.AccessToken)
		if err != nil {
			// panic(err)
			errString := fmt.Sprintf("Error with Create Token Client Get Accounts request: %v \n", err)
			log.Println(errString)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(errString))
		}

		var wg sync.WaitGroup
		for _, account := range pAccountRes.Accounts {
			wg.Add(1)
			go func(pAcc plaid.Account) {
				defer wg.Done()
				acc := types.Account{}
				upsertAccountWithPlaidAccount(acc, pAcc, iTok.Institution, iTok.ItemID, astmt)
			}(account)
		}
		wg.Wait()

		errtx := txn.Commit()
		if errtx != nil {
			errString := fmt.Sprintf("Error with Create Token Txn Commit: %v \n", errtx)
			log.Println(errString)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(errString))
		}

		_, err2 := res.Write([]byte("Upserted " + iTok.Institution))
		if err2 != nil {
			errString := fmt.Sprintf("Error with Create Token Response Write: %v \n", err)
			log.Println(errString)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(errString))
		}
	}
}
*/
func upsertItemToken(tok types.ItemToken, stmt *sqlx.NamedStmt) {

	stmt.MustExec(tok)

}

func upsertAccountWithPlaidAccount(acc types.Account, pAcc plaid.AccountBase, inst string, itemID string, stmt *sqlx.NamedStmt) {
	/*acc.Name = pAcc.Name
	acc.Institution = inst
	acc.AccountID = pAcc.AccountId
	acc.Provider = "Plaid"
	if pAcc.Type == "credit" {
		acc.Balance = decimal.NewFromFloat(pAcc.Balances.Current * -1)
	} else {
		acc.Balance = decimal.NewFromFloat32(pAcc.Balances.Current)
	}
	acc.Limit = decimal.NewFromFloat32(pAcc.Balances.Limit)
	acc.Available = decimal.NewFromFloat32(pAcc.Balances.Available)
	acc.Currency = pAcc.Balances.ISOCurrencyCode
	acc.Type = pAcc.Type
	acc.Subtype = pAcc.Subtype
	acc.ItemID = itemID

	stmt.MustExec(acc)*/

}

/*
func GeneratePublicTokenFunction() func(http.ResponseWriter, *http.Request) {
	return func(res http.ResponseWriter, req *http.Request) {

		var err error

		decoder := json.NewDecoder(req.Body)
		var item types.GenerateTokenPost
		err = decoder.Decode(&item)
		if err != nil {
			// panic(err)
			errString := fmt.Sprintf("Error with Generate Token Post Request: %v \n", err)
			log.Println(errString)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(errString))
		}

		var access string

		query := fmt.Sprintf(`SELECT access_token FROM item_tokens WHERE item_id = %q`, item.ItemID)
		err = db.DBCon.Get(&access, query)
		if err != nil {
			// panic(err)
			errString := fmt.Sprintf("Error with Generate Token Item Query: %v \n", err)
			log.Println(errString)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(errString))
		}

		pClient, err := newClient()
		if err != nil {
			// panic(err)
			errString := fmt.Sprintf("Error with Create Token Client Create: %v \n", err)
			log.Println(errString)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(errString))
		}

		pRes, err := pClient.CreateLinkToken(LinkTokenConfigs{
			User: &LinkTokenUser{
				ClientUserID:             time.Now().String(),
				LegalName:                "Fin User",
				PhoneNumber:              "8008675309",
				EmailAddress:             "test@email.com",
				PhoneNumberVerifiedTime:  time.Now(),
				EmailAddressVerifiedTime: time.Now(),
			},
			ClientName:   "Plaid Test",
			Products:     []string{"auth"},
			CountryCodes: []string{"US"},
			AccessToken:  access,
			Webhook:      "https://webhook-uri.com",
			AccountFilters: &map[string]map[string][]string{
				"depository": {
					"account_subtypes": {"all"},
				},
			},
			Language:              "en",
			LinkCustomizationName: "default",
		})
		if err != nil {
			// panic(err)
			errString := fmt.Sprintf("Error with Create Link Token: %v \n", err)
			log.Println(errString)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(errString))
		}

		// Deprecated in favor of Link (above)

		// pRes, err := pClient.CreatePublicToken(access)
		// if err != nil {
		// 	// panic(err)
		// 	errString := fmt.Sprintf("Error with Generate Token Client Create Token request: %v \n", err)
		// 	log.Println(errString)
		// 	res.WriteHeader(http.StatusInternalServerError)
		// 	res.Write([]byte(errString))
		// }

		if pRes.PublicToken == "" {
			errString := fmt.Sprintf("PublicToken response seems to be empty")
			log.Println(errString)
			res.WriteHeader(http.StatusInternalServerError)
			res.Write([]byte(errString))
		} else {
			resJSON := `{"public_token": "` + pRes.PublicToken + `"}`
			_, err2 := res.Write([]byte(resJSON))
			if err2 != nil {
				errString := fmt.Sprintf("Error with Plaid Generate Public Token: %v \n", err2)
				log.Println(errString)
				res.WriteHeader(http.StatusInternalServerError)
				res.Write([]byte(errString))
			}
		}

	}
}
*/
func RefreshConnection(iTok types.ItemToken, istmt, astmt *sqlx.NamedStmt) {
	ctx := context.Background()
	pAccountRes, _, err := client.PlaidApi.AccountsGet(ctx).AccountsGetRequest(
		*plaid.NewAccountsGetRequest(accessToken),
	).Execute()

	if err != nil {
		// perr, ok := err.(plaid.Error)
		// if ok {
		// 	log.Println(fmt.Sprintf("Plaid error: %v", perr))
		// 	if perr.ErrorCode == "ITEM_LOGIN_REQUIRED" {
		// 		iTok.NeedsReLogin = true
		// 		upsertItemToken(iTok, istmt)
		// 		return
		// 	}
		// }
		errString := fmt.Sprintf("Error with Create Token Client Get Accounts request: %v \n", err)
		log.Println(errString)
		panic(err)
	}

	var wgAcc sync.WaitGroup
	for _, account := range pAccountRes.Accounts {
		wgAcc.Add(1)
		go func(pAcc plaid.AccountBase) {
			defer wgAcc.Done()
			acc := types.Account{}
			upsertAccountWithPlaidAccount(acc, pAcc, iTok.Institution, iTok.ItemID, astmt)
		}(account)
	}
	wgAcc.Wait()
}

func FetchTransactionsForItemToken(iTok types.ItemToken, istmt *sqlx.NamedStmt, astmt *sqlx.NamedStmt, tstmt *sqlx.NamedStmt, baseCurrency string) {
	/*today := time.Now().Format("2006-01-02")

	var pTransRes = Transactions()
	if iTok.LastDownloadedTransactions.IsZero() {
		pTransRes, err = pClient.TransactionsGet(iTok.AccessToken, "2000-01-01", today)
	} else {
		pTransRes, err = pClient.TransactionsGet(iTok.AccessToken, iTok.LastDownloadedTransactions.AddDate(0, 0, -40).Format("2006-01-02"), today)
	}
	if err != nil {
		perr, ok := err.(plaid.Error)
		if ok {
			if perr.ErrorCode == "ITEM_LOGIN_REQUIRED" {
				return
			}
		}
		panic(err)
	}

	for _, ptx := range pTransRes.Transactions {
		tx := types.Transaction{}

		tx.Date = ptx.Date
		tx.TransactionID = ptx.ID
		tx.Description = ptx.Name
		tx.Amount = decimal.NewFromFloat(ptx.Amount * -1)
		tx.CurrencyCode = ptx.ISOCurrencyCode
		tx.NormalizedAmount = db.GetNormalizedAmount(tx.CurrencyCode, baseCurrency, tx.Date, tx.Amount)

		//Searching for category ID match first
		pCat := types.CategoryPlaid{}
		query := fmt.Sprintf(`SELECT * FROM plaid__categories WHERE cat_i_d = %q`, ptx.CategoryID)
		err = db.DBCon.Get(&pCat, query)
		if err != nil && err != sql.ErrNoRows {
			panic(err)
		}
		if err == sql.ErrNoRows {
			//If still nil then set category to Uncategorized
			tx.Category = 106
			tx.CategoryName = "Uncategorized"
		} else {
			tx.Category = pCat.LinkToAppCat
			tx.CategoryName = pCat.AppCatName
		}

		var name string
		err = db.DBCon.Get(&name, "SELECT name FROM accounts WHERE account_id='"+ptx.AccountID+"' AND provider='Plaid' LIMIT 1")
		if err != nil {
			panic(err)
		}
		tx.AccountName = name
		tx.AccountID = ptx.AccountID

		tstmt.MustExec(tx)
	}

	iTok.LastDownloadedTransactions = time.Now()
	upsertItemToken(iTok, istmt)*/
}
