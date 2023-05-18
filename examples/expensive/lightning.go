package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	lnsocket "github.com/jb55/lnsocket/go"
	"github.com/tidwall/gjson"
)

func generateLabel(pubkey string) string { return fmt.Sprintf("relayer-expensive:ticket:%s", pubkey) }

func generateInvoice(r *Relay, pubkey string) (string, error) {
	label := generateLabel(pubkey)
	cln := lnsocket.LNSocket{}
	cln.GenKey()

	err := cln.ConnectAndInit(r.CLNHost, r.CLNNodeId)
	if err != nil {
		return "", err
	}
	defer cln.Disconnect()

	// check if there is an invoice already
	jparams, _ := json.Marshal(map[string]any{
		"label": label,
	})
	result, _ := cln.Rpc(r.CLNRune, "listinvoices", string(jparams))
	if gjson.Get(result, "result.invoices.#").Int() == 1 {
		timestamp := time.Now().Unix()
		if (gjson.Get(result, "result.invoices.0.expires_at").Int() > timestamp) {
			return gjson.Get(result, "result.invoices.0.bolt11").String(), nil
		}
		jparams, _ := json.Marshal(map[string]any{
			"label": label,
			"status": "expired",
		})
		cln.Rpc(r.CLNRune, "delinvoice", string(jparams))
	}

	// otherwise generate an invoice
	jparams, _ = json.Marshal(map[string]any{
		"amount_msat": r.TicketPriceSats * 1000,
		"label":       label,
		"description": fmt.Sprintf("%s's ticket for writing to relayer-expensive", pubkey),
	})
	result, err = cln.Rpc(r.CLNRune, "invoice", string(jparams))
	if err != nil {
		return "", err
	}

	resErr := gjson.Get(result, "error")
	if resErr.Type != gjson.Null {
		if resErr.Type == gjson.JSON {
			return "", errors.New(resErr.Get("message").String())
		} else if resErr.Type == gjson.String {
			return "", errors.New(resErr.String())
		}
		return "", fmt.Errorf("Unknown commando error: '%v'", resErr)
	}

	invoice := gjson.Get(result, "result.bolt11")
	if invoice.Type != gjson.String {
		return "", fmt.Errorf("No bolt11 result found in invoice response, got %v", result)
	}

	return invoice.String(), nil
}

func checkInvoicePaidOk(pubkey string) bool {
	cln := lnsocket.LNSocket{}
	cln.GenKey()

	err := cln.ConnectAndInit(r.CLNHost, r.CLNNodeId)
	if err != nil {
		return false
	}
	defer cln.Disconnect()

	jparams, _ := json.Marshal(map[string]any{
		"label": generateLabel(pubkey),
	})
	result, _ := cln.Rpc(r.CLNRune, "listinvoices", string(jparams))

	return gjson.Get(result, "result.invoices.0.status").String() == "paid"
}
