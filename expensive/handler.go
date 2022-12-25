package main

import (
	"encoding/json"
	"net/http"
)

func handleWebpage(w http.ResponseWriter, rq *http.Request) {
	w.Header().Set("Content-Type", "text/html")
	w.Write([]byte(`
<meta charset=utf-8>
<title>expensive relay</title>
<h1>expensive relay</h1>
<a href="https://github.com/fiatjaf/expensive-relay">https://github.com/fiatjaf/expensive-relay</a>
<p>this is a nostr relay that only accepts events published from keys that pay a registration fee. this is an antispam measure. you can still be banned if you're spamming or doing something bad.</p>
<p>to register your nostr public key, type it below and click the button.</p>
<form>
  <label>
    nostr public key:
    <input name=pubkey />
  </label>
  <button>Get Invoice</button>
</form>
<p id=message></p>
<a id=link><canvas id=qr /></a>
<code id=invoice></code>
<script src="https://cdnjs.cloudflare.com/ajax/libs/qrious/4.0.2/qrious.min.js"></script>
<script>
document.querySelector('form').addEventListener('submit', async ev => {
  ev.preventDefault()
  let res = await (await fetch('/invoice?pubkey=' + ev.target.pubkey.value)).text()
  let { bolt11, error } = JSON.parse(res)
  if (bolt11) {
    invoice.innerHTML = bolt11
    link.href = 'lightning:' + bolt11
    new QRious({
      element: qr,
      value: bolt11.toUpperCase(),
      size: 300
    });
  } else {
    message.innerHTML = error
  }
})
</script>
<style>
body {
  margin: 10px auto;
  width: 800px;
  max-width: 90%;
}
</style>
    `))
}

func handleInvoice(w http.ResponseWriter, rq *http.Request, r *Relay) {
	w.Header().Set("Content-Type", "application/json")
	invoice, err := generateInvoice(r, rq.URL.Query().Get("pubkey"))
	if err != nil {
		json.NewEncoder(w).Encode(struct {
			Error string `json:"error"`
		}{err.Error()})
	} else {
		json.NewEncoder(w).Encode(struct {
			Invoice string `json:"bolt11"`
		}{invoice})
	}
}
