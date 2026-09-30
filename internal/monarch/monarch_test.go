package monarch

import (
	"strings"
	"testing"
)

const txnCSV = "\uFEFFDate,Merchant,Category,Account,Original Statement,Notes,Amount,Tags,Owner,Reviewed,Id\n" +
	`2026-09-30,Sal's Pizza,Restaurants & Bars,Checking (...2080),TST SALS PIZZA,,-55.72,,Shared,,1
2026-09-29,Acme,Paychecks,Checking (...2080),ACME PAYROLL,"note, with comma","1,200.00","Work, Pension",Shared,Needs Review,2
9/28/2026,Mystery,Uncategorized,Card,X,,-5,,Shared,Reviewed,3
`

func TestParseTransactions(t *testing.T) {
	txns, err := ParseTransactions(strings.NewReader(txnCSV))
	if err != nil {
		t.Fatal(err)
	}
	if len(txns) != 3 {
		t.Fatalf("rows = %d", len(txns))
	}
	a, b, c := txns[0], txns[1], txns[2]
	if a.Amount != -5572 || a.Merchant != "Sal's Pizza" || a.Statement != "TST SALS PIZZA" || a.ID != "1" {
		t.Errorf("row 1 = %+v", a)
	}
	if b.Amount != 120000 || b.Notes != "note, with comma" || strings.Join(b.Tags, "|") != "Work|Pension" || !b.NeedsReview {
		t.Errorf("row 2 = %+v", b)
	}
	if c.Date != "2026-09-28" || c.Category != "" || c.NeedsReview {
		t.Errorf("row 3 = %+v", c)
	}
	if _, err := ParseTransactions(strings.NewReader("Date,Balance,Account\n")); err == nil || !strings.Contains(err.Error(), "merchant") {
		t.Errorf("balances file as transactions: %v", err)
	}
	if _, err := ParseTransactions(strings.NewReader("Date,Merchant,Category,Account,Amount\n2026-01-01,x,y,z,abc\n")); err == nil || !strings.Contains(err.Error(), "line 2") {
		t.Errorf("bad amount: %v", err)
	}
}

func TestAccounts(t *testing.T) {
	txns, _ := ParseTransactions(strings.NewReader(txnCSV))
	bals, err := ParseBalances(strings.NewReader("Date,Balance,Account\n2026-09-01,100.00,Checking (...2080)\n2026-09-30,42.25,Checking (...2080)\n2026-09-30,-9000,Honda CRV (...8141)\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := Accounts(txns, bals)
	if len(got) != 3 {
		t.Fatalf("accounts = %+v", got)
	}
	chk, card, honda := got[0], got[1], got[2]
	if chk.Name != "Checking" || chk.Mask != "2080" || chk.Transactions != 2 || chk.LatestBalance != 4225 || chk.Balances != 2 || chk.Type != "checking" {
		t.Errorf("checking = %+v", chk)
	}
	// No balances: the sum of its transactions.
	if card.Key != "Card" || card.LatestBalance != -500 || card.HasBalance || card.Type != "credit_card" {
		t.Errorf("card = %+v", card)
	}
	// A liability with no transactions is suggested as a loan.
	if honda.Name != "Honda CRV" || honda.Type != "loan" || honda.LatestBalance != -900000 {
		t.Errorf("honda = %+v", honda)
	}
}

func TestSuggest(t *testing.T) {
	if k, i := Suggest("Cash & ATM"); k != "flexible" || i != "🏧" {
		t.Errorf("cash = %s %s", k, i)
	}
	if k, _ := Suggest("Side Hustle Income"); k != "income" {
		t.Errorf("income guess = %s", k)
	}
}
