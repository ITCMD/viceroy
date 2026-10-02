package debt

import "testing"

func TestMonthlyInterest(t *testing.T) {
	if got := MonthlyInterest(120000, 1200); got != 1200 { // $1,200 at 12% = $12/month
		t.Fatalf("interest = %d", got)
	}
	if MonthlyInterest(-5, 1200) != 0 || MonthlyInterest(5000, 0) != 0 {
		t.Fatal("no interest on nothing owed or 0%")
	}
}

func TestOrder(t *testing.T) {
	ds := []Debt{{ID: 1, Balance: 500000, APRBps: 2400}, {ID: 2, Balance: 100000, APRBps: 600}, {ID: 3, Balance: 300000, APRBps: 2900}}
	ids := func(s Strategy) []int64 {
		var out []int64
		for _, i := range Order(ds, s) {
			out = append(out, ds[i].ID)
		}
		return out
	}
	if got := ids(Snowball); got[0] != 2 || got[1] != 3 || got[2] != 1 {
		t.Fatalf("snowball order = %v", got)
	}
	if got := ids(Avalanche); got[0] != 3 || got[1] != 1 || got[2] != 2 {
		t.Fatalf("avalanche order = %v", got)
	}
}

func TestSimulate(t *testing.T) {
	ds := []Debt{
		{ID: 1, Name: "Card", Balance: 300000, APRBps: 2400, MinPayment: 9000},
		{ID: 2, Name: "Car", Balance: 800000, APRBps: 500, MinPayment: 25000},
	}
	minimum := Simulate(ds, Minimum, 50000)
	snow := Simulate(ds, Snowball, 20000)
	ava := Simulate(ds, Avalanche, 20000)
	if minimum.Never || snow.Never || ava.Never {
		t.Fatal("every plan should finish")
	}
	if minimum.Payment != 34000 || snow.Payment != 54000 {
		t.Fatalf("payments = %d, %d", minimum.Payment, snow.Payment)
	}
	if !(snow.Months < minimum.Months && snow.Interest < minimum.Interest) {
		t.Fatalf("extra should finish sooner and cheaper: min %d mo $%d, snowball %d mo $%d", minimum.Months, minimum.Interest, snow.Months, snow.Interest)
	}
	if ava.Interest > snow.Interest {
		t.Fatalf("avalanche interest %d > snowball %d", ava.Interest, snow.Interest)
	}
	if got := snow.Balances[len(snow.Balances)-1]; got != 0 || len(snow.Balances) != snow.Months {
		t.Fatalf("balances end at %d after %d months (plan %d)", got, len(snow.Balances), snow.Months)
	}
	// Smallest balance first: the card is targeted and paid well before the car.
	if snow.Debts[0].Order != 1 || snow.Debts[0].Months >= snow.Debts[1].Months {
		t.Fatalf("snowball payoffs = %+v", snow.Debts)
	}
	var sum int64
	for _, d := range snow.Debts {
		sum += d.Interest
	}
	if sum != snow.Interest {
		t.Fatalf("per-debt interest %d != total %d", sum, snow.Interest)
	}
}

func TestSimulateNever(t *testing.T) {
	// $10,000 at 24% costs $200/month; a $150 minimum never catches up.
	p := Simulate([]Debt{{ID: 1, Balance: 1000000, APRBps: 2400, MinPayment: 15000}}, Minimum, 0)
	if !p.Never || p.Months != 0 || p.Debts[0].Months != 0 || len(p.Balances) != MaxMonths {
		t.Fatalf("plan = never %v months %d", p.Never, p.Months)
	}
	if p := Simulate([]Debt{{ID: 1, Balance: 1000000, APRBps: 2400, MinPayment: 15000}}, Snowball, 20000); p.Never {
		t.Fatal("with $200 extra it gets paid")
	}
}
