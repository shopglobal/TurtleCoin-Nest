// Package walletdmanager handles the management of the wallet and the communication with the core wallet software
package walletdmanager

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	log "github.com/sirupsen/logrus"
)

type rpcPayload struct {
	JSONRPC  string                  `json:"jsonrpc"`
	Method   string                  `json:"method"`
	Params   *map[string]interface{} `json:"params,omitempty"`
	Password string                  `json:"password"`
	ID       int                     `json:"id"`
}

// Transfer contains all the information about a specific transfer
type Transfer struct {
	PaymentID              string
	TxID                   string
	Timestamp              time.Time
	Amount                 float64
	Fee                    float64
	Block                  int
	Confirmations          int
	IsRecievingTransaction bool
}

var (
	rpcURL = "http://127.0.0.1:8070/json_rpc"

	walletdCurrentSessionLogFileFilename = "walletdCurrentSession.log"
	walletdLogFileFilename               = "walletd.log"

	walletTotalBalance float64
	// WalletAvailableBalance is the available balance
	WalletAvailableBalance float64
	walletLockedBalance    float64

	// WalletAddress is the wallet address
	WalletAddress string

	// WalletFilename is the filename of the opened wallet
	WalletFilename = ""

	// will be set to a random string when starting walletd
	rpcPassword = ""

	// Transfers is a slice with all the history of transactions of the opened wallet
	Transfers []Transfer

	cmdWalletd *exec.Cmd

	// WalletdOpenAndRunning is true when walletd is running with a wallet open
	WalletdOpenAndRunning = false

	// WalletdSynced is true when wallet is synced and transfer is allowed
	WalletdSynced = false
)

// RequestBalance provides the available and locked balances of the current wallet
func RequestBalance() (availableBalance float64, lockedBalance float64, totalBalance float64) {

	args := make(map[string]interface{})

	payload := rpcPayload{
		JSONRPC:  "2.0",
		Method:   "getBalance",
		Params:   &args,
		Password: rpcPassword,
		ID:       1}

	payloadjson, err := json.Marshal(payload)
	if err != nil {
		log.Fatal("error json marshal: ", err)
	}

	req, err := http.NewRequest("POST", rpcURL, bytes.NewBuffer(payloadjson))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("error http request: ", err)
	}
	defer resp.Body.Close()

	log.Info("response Status getBalance:", resp.Status)
	responseBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {

		log.Fatal("error reading result from rpc request getBalance:", err)

	} else {

		var responseBodyInterface interface{}
		if err := json.Unmarshal(responseBody, &responseBodyInterface); err != nil {

			log.Fatal("JSON unmarshaling with interface failed:", err)

		} else {

			responseMap := responseBodyInterface.(map[string]interface{})

			WalletAvailableBalance = responseMap["result"].(map[string]interface{})["availableBalance"].(float64) / 100
			walletLockedBalance = responseMap["result"].(map[string]interface{})["lockedAmount"].(float64) / 100
			walletTotalBalance = WalletAvailableBalance + walletLockedBalance

			return WalletAvailableBalance, walletLockedBalance, walletTotalBalance

		}

	}

	return 0, 0, 0

}

// RequestAddress provides the address of the current wallet
func RequestAddress() string {

	args := make(map[string]interface{})

	payload := rpcPayload{
		JSONRPC:  "2.0",
		Method:   "getAddresses",
		Params:   &args,
		Password: rpcPassword,
		ID:       2}

	payloadjson, err := json.Marshal(payload)
	if err != nil {
		log.Fatal("error json marshal: ", err)
	}

	req, err := http.NewRequest("POST", rpcURL, bytes.NewBuffer(payloadjson))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("error http request: ", err)
	}
	defer resp.Body.Close()

	log.Info("response Status getAddresses:", resp.Status)
	responseBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {

		log.Fatal("error reading result from rpc request getAddresses:", err)

	} else {

		var responseBodyInterface interface{}
		if err := json.Unmarshal(responseBody, &responseBodyInterface); err != nil {

			log.Fatal("JSON unmarshaling with interface failed:", err)

		} else {

			responseMap := responseBodyInterface.(map[string]interface{})

			walletAddresses := responseMap["result"].(map[string]interface{})["addresses"].([]interface{})

			WalletAddress = walletAddresses[0].(string)

			return WalletAddress

		}

	}

	return "error displaying the address"

}

// RequestListTransactions provides the list of transactions of current wallet
func RequestListTransactions() (transfers []Transfer) {

	args := make(map[string]interface{})

	walletBlockCount, _, _, err := requestStatus()
	if err != nil {
		log.Fatal("error getting block count: ", err)
	}

	args["blockCount"] = walletBlockCount
	args["firstBlockIndex"] = 1
	args["addresses"] = []string{WalletAddress}

	// Request all transactions related to our addresses from the wallet
	// This returns a list of blocks with only our transactions populated in them

	payload := rpcPayload{
		JSONRPC:  "2.0",
		Method:   "getTransactions",
		Params:   &args,
		Password: rpcPassword,
		ID:       3}

	payloadjson, err := json.Marshal(payload)
	if err != nil {
		log.Fatal("error json marshal: ", err)
	}

	req, err := http.NewRequest("POST", rpcURL, bytes.NewBuffer(payloadjson))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("error http request: ", err)
	}
	defer resp.Body.Close()

	log.Info("response Status getTransactions:", resp.Status)
	responseBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {

		log.Fatal("error reading result from rpc request getAddresses:", err)

	} else {

		var responseBodyInterface interface{}
		if err := json.Unmarshal(responseBody, &responseBodyInterface); err != nil {

			log.Fatal("JSON unmarshaling with interface failed:", err)

		} else {

			responseMap := responseBodyInterface.(map[string]interface{})

			blocks := responseMap["result"].(map[string]interface{})["items"].([]interface{})

			for _, block := range blocks {

				transactions := block.(map[string]interface{})["transactions"].([]interface{})

				for _, transaction := range transactions {

					mapTransaction := transaction.(map[string]interface{})

					var transfer Transfer
					transfer.PaymentID = mapTransaction["paymentId"].(string)
					transfer.TxID = mapTransaction["transactionHash"].(string)
					transfer.Timestamp = time.Unix(int64(mapTransaction["timestamp"].(float64)), 0)
					transfer.Amount = mapTransaction["amount"].(float64) / 100
					transfer.Fee = mapTransaction["fee"].(float64) / 100
					transfer.Block = int(mapTransaction["blockIndex"].(float64))
					transfer.Confirmations = walletBlockCount - transfer.Block + 1
					transfer.IsRecievingTransaction = transfer.Amount >= 0

					transfers = append(transfers, transfer)

				}

			}

			return transfers

		}

	}

	return nil

}

func requestStatus() (blockCount int, knownBlockCount int, peerCount int, err error) {

	args := make(map[string]interface{})

	payload := rpcPayload{
		JSONRPC:  "2.0",
		Method:   "getStatus",
		Params:   &args,
		Password: rpcPassword,
		ID:       4}

	payloadjson, err := json.Marshal(payload)
	if err != nil {
		log.Error("error json marshal: ", err)
		return 0, 0, 0, errors.New("error json marshal: " + err.Error())
	}

	req, err := http.NewRequest("POST", rpcURL, bytes.NewBuffer(payloadjson))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Error("error http request: ", err)
		return 0, 0, 0, errors.New("error http request: " + err.Error())
	}
	defer resp.Body.Close()

	log.Info("response Status getStatus:", resp.Status)
	responseBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {

		log.Error("error reading result from rpc request getAddresses:", err)
		return 0, 0, 0, errors.New("error reading result from rpc request getAddresses:" + err.Error())

	}

	var responseBodyInterface interface{}
	if err := json.Unmarshal(responseBody, &responseBodyInterface); err != nil {

		log.Error("JSON unmarshaling with interface failed:", err)
		return 0, 0, 0, errors.New("JSON unmarshaling with interface failed:" + err.Error())

	}

	responseMap := responseBodyInterface.(map[string]interface{})

	blockCount = int(responseMap["result"].(map[string]interface{})["blockCount"].(float64))
	knownBlockCount = int(responseMap["result"].(map[string]interface{})["knownBlockCount"].(float64))
	peerCount = int(responseMap["result"].(map[string]interface{})["peerCount"].(float64))

	return blockCount, knownBlockCount, peerCount, nil

}

// SendTransaction makes a transfer with the provided information
func SendTransaction(transferAddress string, transferAmountString string, transferPaymentID string) (transactionHash string, err error) {

	if !strings.HasPrefix(transferAddress, "XS") {

		return "", errors.New("address is invalid")

	}

	if transferAddress == WalletAddress {

		return "", errors.New("sending to yourself is not supported")

	}

	var transferFee float64 = 400000000 // transferFee is expressed in TRTL
	transferMixin :=54

	transferAmount, err := strconv.ParseFloat(transferAmountString, 64) // transferAmount is expressed in TRTL

	if err != nil {

		return "", errors.New("amount is invalid")

	}

	if transferAmount <= 0 {

		return "", errors.New("amount of XSM to be sent should be greater than 0")

	}

	if transferAmount+transferFee > WalletAvailableBalance {

		return "", errors.New("your available balance is insufficient")

	}

	if transferAmount > 5000000 {

		return "", errors.New("for sending more than 5,000,000 XSN to one address, you should split in multiple transfers of smaller amounts")

	}

	transferAmountInt := int(transferAmount * 1000000000) // transferAmountInt is expressed in hundredth of XSM
	transferFeeInt := int(transferFee * 1000000000)       // transferFeeInt is expressed in hundredth of XSM

	args := make(map[string]interface{})

	args["fee"] = transferFeeInt
	args["paymentId"] = transferPaymentID
	args["anonymity"] = transferMixin

	var transfers [1]map[string]interface{}
	transfer := make(map[string]interface{})
	transfer["amount"] = transferAmountInt
	transfer["address"] = transferAddress
	transfers[0] = transfer
	args["transfers"] = transfers

	payload := rpcPayload{
		JSONRPC:  "2.0",
		Method:   "sendTransaction",
		Params:   &args,
		Password: rpcPassword,
		ID:       5}

	payloadjson, err := json.Marshal(payload)
	if err != nil {
		log.Fatal("error json marshal: ", err)
	}

	req, err := http.NewRequest("POST", rpcURL, bytes.NewBuffer(payloadjson))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("error http request: ", err)
	}
	defer resp.Body.Close()

	log.Info("response Status sendTransaction:", resp.Status)
	responseBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {

		log.Fatal("error reading result from rpc request sendTransaction:", err)

	} else {

		var responseBodyInterface interface{}
		if err := json.Unmarshal(responseBody, &responseBodyInterface); err != nil {

			log.Fatal("JSON unmarshaling with interface failed:", err)

		} else {

			responseMap := responseBodyInterface.(map[string]interface{})

			responseError := responseMap["error"]

			if responseError != nil {

				return "", errors.New(responseError.(map[string]interface{})["message"].(string))

			}

			return responseMap["result"].(map[string]interface{})["transactionHash"].(string), nil

		}

	}

	return "", errors.New("unknown error")

}

// getPrivateViewKey provides the private view key of the current wallet
func getPrivateViewKey() (privateViewKey string, err error) {

	args := make(map[string]interface{})

	payload := rpcPayload{
		JSONRPC:  "2.0",
		Method:   "getViewKey",
		Params:   &args,
		Password: rpcPassword,
		ID:       6}

	payloadjson, err := json.Marshal(payload)
	if err != nil {
		log.Fatal("error json marshal: ", err)
	}

	req, err := http.NewRequest("POST", rpcURL, bytes.NewBuffer(payloadjson))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("error http request: ", err)
	}
	defer resp.Body.Close()

	log.Info("response Status getViewKey:", resp.Status)
	responseBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {

		log.Fatal("error reading result from rpc request getViewKey:", err)

	} else {

		var responseBodyInterface interface{}
		if err := json.Unmarshal(responseBody, &responseBodyInterface); err != nil {

			log.Fatal("JSON unmarshaling with interface failed:", err)

		} else {

			responseMap := responseBodyInterface.(map[string]interface{})

			privateViewKey = responseMap["result"].(map[string]interface{})["viewSecretKey"].(string)

			return privateViewKey, nil

		}

	}

	return "", errors.New("unknown error")

}

// getPrivateSpendKey provides the private view of the current wallet
func getPrivateSpendKey() (privateSpendKey string, err error) {

	args := make(map[string]interface{})

	args["address"] = WalletAddress

	payload := rpcPayload{
		JSONRPC:  "2.0",
		Method:   "getSpendKeys",
		Params:   &args,
		Password: rpcPassword,
		ID:       7}

	payloadjson, err := json.Marshal(payload)
	if err != nil {
		log.Fatal("error json marshal: ", err)
	}

	req, err := http.NewRequest("POST", rpcURL, bytes.NewBuffer(payloadjson))

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		log.Fatal("error http request: ", err)
	}
	defer resp.Body.Close()

	log.Info("response Status getSpendKey:", resp.Status)
	responseBody, err := ioutil.ReadAll(resp.Body)

	if err != nil {

		log.Fatal("error reading result from rpc request getSpendKey:", err)

	} else {

		var responseBodyInterface interface{}
		if err := json.Unmarshal(responseBody, &responseBodyInterface); err != nil {

			log.Fatal("JSON unmarshaling with interface failed:", err)

		} else {

			responseMap := responseBodyInterface.(map[string]interface{})

			privateSpendKey = responseMap["result"].(map[string]interface{})["spendSecretKey"].(string)

			return privateSpendKey, nil

		}

	}

	return "", errors.New("unknown error")

}

// GetPrivateViewKeyAndSpendKey provides the private view and spend keys of the current wallet
func GetPrivateViewKeyAndSpendKey() (privateViewKey string, privateSpendKey string, err error) {

	privateViewKey, err = getPrivateViewKey()

	if err != nil {

		return "", "", err

	}

	privateSpendKey, err = getPrivateSpendKey()

	if err != nil {

		return "", "", err

	}

	return privateViewKey, privateSpendKey, nil

}

// StartWalletd starts the walletd daemon with the set wallet info
// walletPath is the full path to the wallet
// walletPassword is the wallet password
func StartWalletd(walletPath string, walletPassword string) (err error) {

	fileExtension := filepath.Ext(walletPath)

	if fileExtension != ".wallet" {

		return errors.New("filename should end with .wallet")

	}

	WalletFilename = filepath.Base(walletPath)

	walletFilePath := filepath.Clean(walletPath)

	walletFilePath = strings.Replace(walletFilePath, "file:", "", 1)

	// setup current session log file (logs are added real time in this file)
	walletdCurrentSessionLogFile, err := os.Create(walletdCurrentSessionLogFileFilename)
	if err != nil {
		log.Error(err)
	}
	defer walletdCurrentSessionLogFile.Close()

	rpcPassword = randStringBytesMaskImprSrc(20)

	cmdWalletd = exec.Command("./walletd", "-w", walletFilePath, "-p", walletPassword, "-l", walletdCurrentSessionLogFileFilename, "--local", "--rpc-password", rpcPassword)

	// setup all sessions log file (logs are added at the end of this file only after walletd has stopped)
	// walletdLogFile, err := os.Open(walletdLogFileFilename)
	walletdLogFile, err := os.OpenFile(walletdLogFileFilename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Fatal(err)
	}
	cmdWalletd.Stdout = walletdLogFile
	defer walletdLogFile.Close()

	err = cmdWalletd.Start()

	if err != nil {
		log.Error(err)
		return err
	}

	time.Sleep(5 * time.Second)

	reader := bufio.NewReader(walletdCurrentSessionLogFile)

	var listWalletdErrors []string

	for {

		line, err := reader.ReadString('\n')

		if err != nil {

			if err != io.EOF {

				log.Error("Failed reading log file line by line: ", err)

			}

			break
		}

		if strings.Contains(line, " ERROR  ") {

			listWalletdErrors = append(listWalletdErrors, line)

		}

	}

	errorMessage := "Error opening the daemon walletd. Could be a problem with your wallet file, your password or walletd. More info in the file " + walletdLogFileFilename + "\n"

	if len(listWalletdErrors) > 0 {

		for _, line := range listWalletdErrors {

			errorMessage = errorMessage + line

		}

	}

	// check rpc connection with walletd
	_, _, _, err = requestStatus()

	if err != nil {

		return errors.New(errorMessage)

	}

	WalletdOpenAndRunning = true

	return nil
}

// StopWalletd stops the walletd daemon
func StopWalletd() {

	if WalletdOpenAndRunning && cmdWalletd != nil {

		if err := cmdWalletd.Process.Signal(syscall.SIGTERM); err != nil {

			log.Error("failed to kill: ", err)

		} else {

			log.Info("walletd killed without error")

		}

	}

	walletTotalBalance = 0
	WalletAvailableBalance = 0
	walletLockedBalance = 0
	WalletAddress = ""
	WalletFilename = ""
	Transfers = nil
	cmdWalletd = nil
	WalletdOpenAndRunning = false

}

// CreateWallet calls walletd to create a new wallet. If privateViewKey and privateSpendKey are empty strings, a new wallet will be generated. If they are not empty, a wallet will be generated from those keys (import)
// walletFilename is the filename chosen by the user. The created wallet file will be located in the same folder as walletd.
// walletPassword is the password of the new wallet.
// privateViewKey is the private view key of the wallet.
// privateSpendKey is the private spend key of the wallet.
func CreateWallet(walletFilename string, walletPassword string, privateViewKey string, privateSpendKey string) (err error) {

	if WalletdOpenAndRunning {

		return errors.New("walletd is already running. It should be stopped before being able to generate a new wallet")

	}

	if strings.Contains(walletFilename, "/") || strings.Contains(walletFilename, " ") || strings.Contains(walletFilename, ":") {

		return errors.New("you should avoid spaces and most special characters in the filename")

	}

	// setup current session log file (logs are added real time in this file)
	walletdCurrentSessionLogFile, err := os.Create(walletdCurrentSessionLogFileFilename)
	if err != nil {
		log.Error(err)
	}
	defer walletdCurrentSessionLogFile.Close()

	if privateViewKey == "" && privateSpendKey == "" {

		// generate new wallet
		cmdWalletd = exec.Command("./walletd", "-w", walletFilename, "-p", walletPassword, "-l", walletdCurrentSessionLogFileFilename, "-g")

	} else {

		// import wallet from private view and spend keys
		cmdWalletd = exec.Command("./walletd", "-w", walletFilename, "-p", walletPassword, "--view-key", privateViewKey, "--spend-key", privateSpendKey, "-l", walletdCurrentSessionLogFileFilename, "-g")

	}

	// setup all sessions log file
	walletdLogFile, err := os.OpenFile(walletdLogFileFilename, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Fatal(err)
	}
	cmdWalletd.Stdout = walletdLogFile
	defer walletdLogFile.Close()

	err = cmdWalletd.Start()

	if err != nil {
		log.Error(err)
		return err
	}

	time.Sleep(5 * time.Second)

	reader := bufio.NewReader(walletdCurrentSessionLogFile)

	var listWalletdErrors []string

	successCreatingWallet := false

	for {

		line, err := reader.ReadString('\n')

		if err != nil {

			if err != io.EOF {

				log.Error("Failed reading log file line by line: ", err)

			}

			break
		}

		if strings.Contains(line, " ERROR  ") || strings.Contains(line, "error: ") {

			listWalletdErrors = append(listWalletdErrors, line)

		}

		if strings.Contains(line, "New wallet is generated. Address:") || strings.Contains(line, "New wallet added") {

			successCreatingWallet = true

			break

		}

	}

	errorMessage := "Error opening walletd and/or creating a wallet. More info in the file " + walletdLogFileFilename + "\n"

	if !successCreatingWallet {

		if len(listWalletdErrors) > 0 {

			for _, line := range listWalletdErrors {

				errorMessage = errorMessage + line

			}

		}

		return errors.New(errorMessage)

	}

	return nil
}

// RequestConnectionInfo provides the blockchain sync status and the number of connected peers
func RequestConnectionInfo() (syncing string, blockCountString string, knownBlockCountString string, peerCountString string, err error) {

	blockCount, knownBlockCount, peerCount, err := requestStatus()

	if err != nil {

		return "", "", "", "", err

	}

	stringWait := " (No transfers allowed until synced)"

	if knownBlockCount == 0 {

		WalletdSynced = false
		syncing = "Getting block count..." + stringWait

	} else if blockCount < knownBlockCount-1 || blockCount > knownBlockCount+10 {
		// second condition handles cases when knownBlockCount is off and smaller than the blockCount

		WalletdSynced = false
		syncing = "Wallet syncing..." + stringWait

	} else {

		WalletdSynced = true
		syncing = "Wallet synced"

	}

	return syncing, strconv.Itoa(blockCount), strconv.Itoa(knownBlockCount), strconv.Itoa(peerCount), nil

}

// generate a random string with n characters. from https://stackoverflow.com/a/31832326/1668837
func randStringBytesMaskImprSrc(n int) string {

	const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
	const letterIdxBits = 6                    // 6 bits to represent a letter index
	const letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	const letterIdxMax = 63 / letterIdxBits    // # of letter indices fitting in 63 bits

	src := rand.NewSource(time.Now().UnixNano())
	b := make([]byte, n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			cache, remain = src.Int63(), letterIdxMax
		}
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return string(b)
}
