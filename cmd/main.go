package cmd

import (
  "bufio"
  "bytes"
  "encoding/json"
  "flag"
  "fmt"
  "io/ioutil"
  "net/http"
  "os"
  "regexp"
  "strconv"
  "strings"
  "time"

  "github.com/cheggaaa/pb/v3"
  "github.com/piquette/finance-go/quote"
)

type DateFormat struct {
  Format string
}

type CurrencyFormat struct {
  IsoCode          string `json:"iso_code"`
  ExampleFormat    string `json:"example_format"`
  DecimalDigits    int    `json:"decimal_digits"`
  DecimalSeparator string `json:"decimal_separator"`
  SymbolFirst      bool   `json:"symbol_first"`
  GroupSeparator   string `json:"group_separator"`
  CurrencySymbol   string `json:"currency_symbol"`
  DisplaySymbol    bool   `json:"display_symbol"`
}

type Budget struct {
  Id             string
  Name           string
  LastModifiedOn string         `json:"last_modified_on"`
  FirstMonth     string         `json:"first_month"`
  LastMonth      string         `json:"last_month"`
  DateFormat     DateFormat     `json:"date_format"`
  CurrencyFormat CurrencyFormat `json:"currency_format"`
}

type SubTransaction struct {
  Id                    string
  TransactionId         string  `json:"transaction_id"`
  Amount                int64
  Memo                  *string
  PayeeId               *string `json:"payee_id"`
  PayeeName             *string `json:"payee_name"`
  CategoryId            *string `json:"category_id"`
  CategoryName          *string `json:"category_name"`
  TransferAccountId     *string `json:"transfer_account_id"`
  TransferTransactionId *string `json:"transfer_transaction_id"`
  Deleted               bool
}

type Transaction struct {
  Id                    string
  Date                  string
  Amount                int64
  Memo                  *string
  Cleared               string
  Approved              bool
  FlagColor             *string          `json:"flag_color"`
  AccountId             string           `json:"account_id"`
  PayeeId               *string          `json:"payee_id"`
  CategoryId            *string          `json:"category_id"`
  TransferAccountId     *string          `json:"transfer_account_id"`
  TransferTransactionId *string          `json:"transfer_transaction_id"`
  MatchedTransactionId  *string          `json:"matched_transaction_id"`
  ImportId              *string          `json:"import_id"`
  Deleted               bool
  AccountName           string           `json:"account_name"`
  PayeeName             *string          `json:"payee_name"`
  CategoryName          *string           `json:"category_name"`
  Subtransactions       []SubTransaction
}

type ModifiedSubTransaction struct {
  Amount     int64   `json:"amount"`
  PayeeId    *string `json:"payee_id"`
  PayeeName  *string `json:"payee_name"`
  CategoryId *string `json:"category_id"`
  Memo       *string `json:"memo"`
}

type ModifiedTransaction struct {
  Id              string                    `json:"id"`
  AccountId       string                    `json:"account_id"`
  Date            string                    `json:"date"`
  Amount          int64                     `json:"amount"`
  PayeeId         *string                   `json:"payee_id"`
  PayeeName       *string                   `json:"payee_name"`
  CategoryId      *string                   `json:"category_id"`
  Memo            *string                   `json:"memo"`
  Cleared         *string                   `json:"cleared"`
  Approved        *bool                     `json:"approved"`
  FlagColor       *string                   `json:"flag_color"`
  ImportId        *string                   `json:"import_id"`
  Deleted         *bool                     `json:"deleted"`
  Subtransactions *[]ModifiedSubTransaction `json:"subtransactions"`
}

type ErrorDetails struct {
  Id string
  Name string
  Detail string
}

type Budgets struct {
  Budgets       []Budget
  DefaultBudget Budget   `json:"default_budget"`
}

type Transactions struct {
  Transactions    []Transaction
  ServerKnowledge uint64        `json:"server_knowledge"`
}

type BudgetData struct {
  Data *Budgets
}

type TransactionData struct {
  Data *Transactions
}

type Error struct {
  Error ErrorDetails
}

type ModifiedTransactions struct {
  Transactions []*ModifiedTransaction `json:"transactions"`
}

type TransactionConfig struct {
  Symbol string               `json:"symbol"`
  Amount float64              `json:"amount"`
  Data   *ModifiedTransaction `json:"data"`
}

type BudgetConfig struct {
  ServerKnowledge uint64                       `json:"server_knowledge"`
  Transactions    map[string]TransactionConfig `json:"transactions"`
}

type Config struct {
  YnabToken *string                  `json:"ynab_token"`
  Budgets   map[string]*BudgetConfig `json:"budgets"`
}

var (
  config Config
  client = &http.Client{}
  matchMemo *regexp.Regexp
  prices = map[string]float64{}
)

func Execute() error {
  var err error

  matchMemo, err = regexp.Compile("\\$([^$]{1,10}) (-?[0-9.]+)\\$")
  if err != nil {
    return fmt.Errorf("Failed to compile regex.")
  }

  configFile := flag.String("c", "config.json", "Configuration file")
  flag.Parse()

  if err := TryReadConfig(configFile); err != nil {
    return err
  }

  budgets, err := GetBudgets()
  if err != nil {
    return err
  }

  if config.Budgets == nil {
    config.Budgets = map[string]*BudgetConfig{}
  }

  for _, budget := range budgets {
    err = ProcessBudget(budget)
    if err != nil {
      return err 
    }
  }

  res, err := json.Marshal(config)
  if err != nil {
    return err
  }

  err = os.WriteFile(*configFile, res, 0600)
  if err != nil {
    return err
  }

  return nil
}

func TryReadConfig(path *string) error {
  fmt.Printf("Using config file at path %s.\n", *path)
  configData, err := os.ReadFile(*path)
  if err != nil {
    fmt.Print("Config file does not exist. Create one? [y/N] ")

    if TryCreateConfig(path) {
      return nil
    }
    return fmt.Errorf("Config file does not exist.")
  }
  err = json.Unmarshal(configData, &config)
  if err != nil || config.YnabToken == nil {
    fmt.Print("Unable to read config file. Overwrite? [y/N] ")

    if TryCreateConfig(path) {
      return nil
    }
    return fmt.Errorf("Unable to read config file.")
  }

  return nil
}

func TryCreateConfig(path *string) bool {
  reader := bufio.NewReader(os.Stdin)
  input, err := reader.ReadString('\n')
  if err != nil {
    return false
  }
  input = strings.TrimSuffix(input, "\n")

  if input != "y" && input != "Y" {
    return false
  }


  for {
    fmt.Print("YNAB API Token: ")
    input, err = reader.ReadString('\n')
    if err != nil {
      return false
    }
    input = strings.TrimSuffix(input, "\n")

    if input != "" {
      break
    }
  }

  config = Config{
    YnabToken: &input,
  }

  return true
}

func ProcessBudget(budget string) error {
  budgetConfig, ok := config.Budgets[budget]
  if !ok {
    budgetConfig = &BudgetConfig{
      ServerKnowledge: 0,
      Transactions: map[string]TransactionConfig{},
    }
    config.Budgets[budget] = budgetConfig
  }

  modifiedTransactions, err := GetTransactions(budget, budgetConfig)
  if err != nil {
    return err
  }

  requestBody, err := json.Marshal(ModifiedTransactions{
    Transactions: modifiedTransactions,
  })
  if err != nil {
    return err
  }

  if len(modifiedTransactions) == 0 {
    return nil
  }

  request, err := http.NewRequest("PATCH", fmt.Sprintf("https://api.youneedabudget.com/v1/budgets/%s/transactions", budget), bytes.NewBuffer(requestBody))
  request.Header.Set("Authorization", "Bearer " + *config.YnabToken)
  request.Header.Set("Content-Type", "application/json")
  if err != nil {
    return err
  }

  resp, err := client.Do(request)
  if err != nil {
    return err
  }

  defer resp.Body.Close()

  body, err := ioutil.ReadAll(resp.Body)
  if err != nil {
    return err
  }

  var transactionData TransactionData
  err = json.Unmarshal(body, &transactionData)
  if err != nil {
    return err
  }

  if transactionData.Data == nil {
    var errData Error
    err = json.Unmarshal(body, &errData)
    if err != nil {
      return err
    }

    return fmt.Errorf(errData.Error.Detail)
  }

  budgetConfig.ServerKnowledge = transactionData.Data.ServerKnowledge

  return nil
}

func GetBudgets() ([]string, error) {
  request, err := http.NewRequest("GET", "https://api.youneedabudget.com/v1/budgets", nil)
  request.Header.Set("Authorization", "Bearer " + *config.YnabToken)
  if err != nil {
    return nil, err
  }

  resp, err := client.Do(request)
  if err != nil {
    return nil, err
  }

  defer resp.Body.Close()

  body, err := ioutil.ReadAll(resp.Body)
  if err != nil {
    return nil, err
  }

  var budgetData BudgetData
  err = json.Unmarshal(body, &budgetData)
  if err != nil {
    return nil, err
  }

  if budgetData.Data == nil {
    var errData Error
    err = json.Unmarshal(body, &errData)
    if err != nil {
      return nil, err
    }

    return nil, fmt.Errorf(errData.Error.Detail)
  }

  budgets := make([]string, len(budgetData.Data.Budgets))
  for i, budget := range budgetData.Data.Budgets {
    budgets[i] = budget.Id
  }

  return budgets, nil
}

func GetTransactions(budget string, budgetConfig *BudgetConfig) ([]*ModifiedTransaction, error) {
  request, err := http.NewRequest("GET", fmt.Sprintf("https://api.youneedabudget.com/v1/budgets/%s/transactions?last_knowledge_of_server=%d", budget, budgetConfig.ServerKnowledge), nil)
  request.Header.Set("Authorization", "Bearer " + *config.YnabToken)
  if err != nil {
    return nil, err
  }

  resp, err := client.Do(request)
  if err != nil {
    return nil, err
  }

  defer resp.Body.Close()

  body, err := ioutil.ReadAll(resp.Body)
  if err != nil {
    return nil, err
  }

  var transactionData TransactionData
  err = json.Unmarshal(body, &transactionData)
  if err != nil {
    return nil, err
  }

  if transactionData.Data == nil {
    var errData Error
    err = json.Unmarshal(body, &errData)
    if err != nil {
      return nil, err
    }

    return nil, fmt.Errorf(errData.Error.Detail)
  }

  modifiedTransactions := []*ModifiedTransaction{}

  bar := pb.StartNew(len(transactionData.Data.Transactions) + len(budgetConfig.Transactions))

  for _, transaction := range transactionData.Data.Transactions {
    bar.Increment()
    time.Sleep(time.Millisecond)

    if transaction.Deleted || len(transaction.Subtransactions) > 0 || transaction.Memo == nil {
      delete(budgetConfig.Transactions, transaction.Id)
      continue
    }

    match := matchMemo.FindStringSubmatch(*transaction.Memo)
    if len(match) < 3 {
      continue
    }
    symbol := match[1]
    amount, err := strconv.ParseFloat(match[2], 64)
    if err != nil {
      return nil, err
    }

    if (amount > 0) != (transaction.Amount > 0) {
      continue
    }

    if _, ok := budgetConfig.Transactions[transaction.Id]; !ok {
      bar.AddTotal(1)
    }

    budgetConfig.Transactions[transaction.Id] = TransactionConfig{
      Symbol: symbol,
      Amount: amount,
      Data: &ModifiedTransaction{
        Id: transaction.Id,
        AccountId: transaction.AccountId,
        Date: transaction.Date,
        Amount: transaction.Amount,
        PayeeId: transaction.PayeeId,
        PayeeName: transaction.PayeeName,
        CategoryId: transaction.CategoryId,
        Memo: transaction.Memo,
        Cleared: &transaction.Cleared,
        Approved: &transaction.Approved,
        FlagColor: transaction.FlagColor,
        ImportId: transaction.ImportId,
        Subtransactions: nil,
      },
    }
  }

  for _, transaction := range budgetConfig.Transactions {
    bar.Increment()
    time.Sleep(time.Millisecond)

    price, err := GetPrice(transaction.Symbol)
    if err != nil {
      return nil, err
    }

    newAmount := int64(transaction.Amount * price * 1000)
    if newAmount != transaction.Data.Amount {
      transaction.Data.Amount = newAmount
      modifiedTransactions = append(modifiedTransactions, transaction.Data)
    }
  }

  bar.Finish()
  budgetConfig.ServerKnowledge = transactionData.Data.ServerKnowledge

  return modifiedTransactions, nil
}

func GetPrice(symbol string) (float64, error) {
  price, ok := prices[symbol]
  if ok {
    return price, nil
  }

  q, err := quote.Get(symbol)
  if err != nil {
    return 0, err
  }

  prices[symbol] = q.RegularMarketPrice
  return q.RegularMarketPrice, err
}
