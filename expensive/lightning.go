package main

import (
	"encoding/json"
	"errors"
	"fmt"

	lnsocket "github.com/jb55/lnsocket/go"
	"github.com/tidwall/gjson"
)

func generateLabel(pubkey string) string { return fmt.Sprintf("relayer-expensive:ticket:%s", pubkey) }

func generateInvoice(pubkey string) (string, error) {
	cln := lnsocket.LNSocket{}
	cln.GenKey()

	err := cln.ConnectAndInit(r.CLNHost, r.CLNNodeId)
	if err != nil {
		return "", err
	}
	defer cln.Disconnect()

	// check if there is an invoice already
	jparams, _ := json.Marshal(map[string]any{
		"label": generateLabel(pubkey),
	})
	result, _ := cln.Rpc(r.CLNRune, "listinvoices", string(jparams))
	if gjson.Get(result, "invoices.#").Int() == 1 {
		return gjson.Get(result, "invoices.1.bolt11").String(), nil
	}

	// otherwise generate an invoice
	jparams, _ = json.Marshal(map[string]any{
		"amount_msat": r.TicketPriceSats * 1000,
		"label":       generateLabel(pubkey),
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
