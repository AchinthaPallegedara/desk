#!/usr/bin/env python3
"""
End-to-end test of the Phase 1 core use case, driven entirely through MCP the
way Claude/ChatGPT would (OAuth 2.1 + PKCE, dynamic client registration,
Streamable HTTP). Standard library only.

    BASE_URL=http://localhost:3000 \
    OWNER_EMAIL=owner@claviq.test OWNER_PASSWORD=... \
    OTHER_EMAIL=owner@other.test OTHER_PASSWORD=... \
    python3 e2e/mcp_core_flow.py

Writes the links it produced to e2e/.last-run.json for manual review.
"""
import base64, hashlib, http.cookiejar, json, os, re, secrets, sys, time, urllib.parse, urllib.request

BASE = os.environ.get("BASE_URL", "http://localhost:3000").rstrip("/")
RUN = secrets.token_hex(3)  # unique names per run
results = {"links": {}}


def step(msg):
    print(f"\n▶ {msg}")


def ok(msg):
    print(f"  ✓ {msg}")


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *a, **k):
        return None


class Client:
    """One AI client connection for one owner (own cookie jar + tokens)."""

    def __init__(self, name, email, password):
        self.name, self.email, self.password = name, email, password
        self.jar = http.cookiejar.CookieJar()
        self.http = urllib.request.build_opener(urllib.request.HTTPCookieProcessor(self.jar), NoRedirect)
        self.token = None
        self.sid = None
        self.n = 0

    def req(self, method, path, body=None, headers=None, form=False, raw=False):
        headers = dict(headers or {})
        data = None
        if body is not None:
            data = urllib.parse.urlencode(body).encode() if form else json.dumps(body).encode()
            headers["Content-Type"] = "application/x-www-form-urlencoded" if form else "application/json"
        url = path if path.startswith("http") else BASE + path
        r = urllib.request.Request(url, data=data, method=method, headers=headers)
        try:
            resp = self.http.open(r, timeout=60)
            return resp.status, {k.lower(): v for k, v in resp.headers.items()}, resp.read()
        except urllib.error.HTTPError as e:
            return e.code, {k.lower(): v for k, v in e.headers.items()}, e.read()

    # ---- OAuth: discovery -> DCR -> login -> consent -> code -> token ----
    def connect(self):
        st, h, _ = self.req("POST", "/mcp", {"jsonrpc": "2.0", "id": 0, "method": "initialize"})
        assert st == 401, st
        meta_url = re.search(r'resource_metadata="([^"]+)"', h["www-authenticate"]).group(1)
        prm = json.loads(self.req("GET", meta_url)[2])
        asm = json.loads(self.req("GET", prm["authorization_servers"][0].rstrip("/") + "/.well-known/oauth-authorization-server")[2])
        redirect = "http://localhost:53682/callback"
        st, _, b = self.req("POST", asm["registration_endpoint"], {"client_name": self.name, "redirect_uris": [redirect], "token_endpoint_auth_method": "none"})
        assert st == 201, b
        client_id = json.loads(b)["client_id"]
        verifier = secrets.token_urlsafe(48)
        challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b"=").decode()
        q = urllib.parse.urlencode({"response_type": "code", "client_id": client_id, "redirect_uri": redirect,
                                    "code_challenge": challenge, "code_challenge_method": "S256",
                                    "state": secrets.token_urlsafe(16), "scope": "desk offline_access", "resource": prm["resource"]})
        auth_path = urllib.parse.urlparse(asm["authorization_endpoint"]).path
        st, h, _ = self.req("GET", f"{auth_path}?{q}")
        assert st == 302 and "/login" in h["location"], (st, h)
        st, _, b = self.req("POST", "/api/auth/login", {"email": self.email, "password": self.password})
        assert st == 200, b
        st, _, b = self.req("GET", f"{auth_path}?{q}")
        tok = re.search(r'name="consent_token" value="([^"]+)"', b.decode()).group(1)
        st, h, _ = self.req("POST", f"{auth_path}?{q}", {"consent_token": tok, "decision": "allow"}, form=True)
        code = urllib.parse.parse_qs(urllib.parse.urlparse(h["location"]).query)["code"][0]
        token_path = urllib.parse.urlparse(asm["token_endpoint"]).path
        st, _, b = self.req("POST", token_path, {"grant_type": "authorization_code", "code": code, "redirect_uri": redirect,
                                                  "client_id": client_id, "code_verifier": verifier}, form=True)
        assert st == 200, b
        self.token = json.loads(b)["access_token"]
        init = self.rpc("initialize", {"protocolVersion": "2025-06-18", "capabilities": {}, "clientInfo": {"name": self.name, "version": "1"}})
        self.rpc("notifications/initialized", notify=True)
        return init

    # ---- MCP JSON-RPC over Streamable HTTP ----
    def rpc(self, method, params=None, notify=False):
        body = {"jsonrpc": "2.0", "method": method}
        if params is not None:
            body["params"] = params
        if not notify:
            self.n += 1
            body["id"] = self.n
        h = {"Authorization": "Bearer " + self.token, "Accept": "application/json, text/event-stream",
             "Mcp-Protocol-Version": "2025-06-18"}
        if self.sid:
            h["Mcp-Session-Id"] = self.sid
        st, hh, b = self.req("POST", "/mcp", body, h)
        self.sid = self.sid or hh.get("mcp-session-id")
        if notify:
            return None
        txt = b.decode()
        m = re.search(r"^data: (.*)$", txt, re.M)
        msg = json.loads(m.group(1) if m else txt)
        if "error" in msg:
            raise AssertionError(f"{method}: {msg['error']}")
        return msg["result"]

    def tool(self, name, args=None, expect_error=False):
        r = self.rpc("tools/call", {"name": name, "arguments": args or {}})
        text = r["content"][0]["text"] if r.get("content") else ""
        if expect_error:
            assert r.get("isError"), f"{name} should have failed but returned {text[:200]}"
            return text
        assert not r.get("isError"), f"{name} failed: {text}"
        return r.get("structuredContent")


def fetch(url):
    with urllib.request.urlopen(url, timeout=60) as r:
        return r.status, r.headers.get("Content-Type"), r.read()


def main():
    owner = Client(f"Phone AI {RUN}", os.environ["OWNER_EMAIL"], os.environ["OWNER_PASSWORD"])

    step("Owner connects their AI app (OAuth 2.1 + PKCE, dynamic registration)")
    init = owner.connect()
    ok(f"connected to {init['serverInfo']['name']} {init['serverInfo']['version']}")
    tools = {t["name"]: t for t in owner.rpc("tools/list")["tools"]}
    ok(f"{len(tools)} tools available")
    fin = tools["finalise_document"]["annotations"]
    assert fin.get("destructiveHint") is True and fin.get("readOnlyHint") is False
    ok("finalise_document is marked destructive (AI apps ask the owner before calling it)")

    # ---- Core use case: a client calls with a new requirement ----
    step("1–2. One prompt: 'New client Sunil Fernando wants a restaurant website… create the customer and an estimate from our template'")
    s = owner.tool("get_business_settings")
    vat = next(t for t in s["tax_rates"] if t["is_default"])
    ok(f"AI reads settings: default tax {vat['name']} {vat['rate']}%, currency {s['tenant']['currency']}")
    found = owner.tool("find_customers", {"query": f"Sunil Fernando {RUN}"})
    assert found["count"] == 0
    ok("find_customers: no existing customer")
    cust = owner.tool("create_customer", {"name": f"Sunil Fernando {RUN}", "company": "Spice Garden Restaurant",
                                         "phone": "+94 77 555 1234", "email": "sunil@spicegarden.lk",
                                         "address": "88 Hospital Road, Kandy"})
    ok(f"create_customer -> {cust['name']}")
    templates = owner.tool("list_templates", {"doc_type": "estimate"})["templates"]
    default_tpl = next(t for t in templates if t["is_default"])
    ok(f"uses saved template '{default_tpl['name']}' v{default_tpl['current_version']}")
    est = owner.tool("create_estimate", {
        "customer_id": cust["id"],
        "title": "Restaurant website with online reservations",
        "items": [
            {"description": "UX design — home, menu, gallery, contact (4 pages)", "quantity": 4, "unit_price": 35000},
            {"description": "Development with reservation system", "quantity": 1, "unit_price": 280000},
            {"description": "Content upload and SEO setup", "quantity": 1, "unit_price": 45000, "discount_type": "percent", "discount_value": 10},
            {"description": "Annual hosting and maintenance", "quantity": 1, "unit_price": 96000, "is_addon": True},
        ],
        "milestones": [
            {"name": "Advance", "percent": 30, "note": "On signing"},
            {"name": "Design approval", "percent": 20},
            {"name": "Beta site", "percent": 20},
            {"name": "Launch", "percent": 20},
            {"name": "After 30 days", "percent": 10},
        ],
        "notes": "Includes two rounds of design revisions.",
    })
    d = est["document"]
    # 140000 + 280000 + 40500 = 460500; VAT 18% = 82890; total 543390. Add-on excluded.
    assert d["totals"]["total"] == "543390.00", d["totals"]
    assert d["totals"]["addons_total"] == "113280.00", d["totals"]
    assert d["status"] == "draft" and d["number"] is None
    assert [m["amount"] for m in d["milestones"]] == ["163017.00", "108678.00", "108678.00", "108678.00", "54339.00"], d["milestones"]
    assert not d.get("warnings"), d.get("warnings")
    ok(f"draft estimate: total LKR {d['totals']['total']} (add-on {d['totals']['addons_total']} excluded), milestones 30/20/20/20/10 sum exactly")

    step("3. AI replies with a preview link; owner opens it on the phone")
    v1 = est["preview_url"]
    results["links"]["preview_v1"] = v1
    token = v1.split("/p/")[1]
    st, ct, b = fetch(f"{BASE}/api/public/p/{token}")
    info = json.loads(b)
    assert info["status"] == "draft" and info["pages"] >= 1
    st, ct, png = fetch(f"{BASE}/api/public/p/{token}/pages/1")
    assert ct == "image/png" and len(png) > 20000
    ok(f"preview opens without login: {info['pages']} page(s), DRAFT, page image {len(png)//1024} KB")
    st, _, _ = Client("x", "", "").req("GET", f"/api/public/p/{token[:-2]}xx")
    assert st == 404
    ok("tampered link rejected")

    step("4. Owner chats edits: 'make development 300k, drop the SEO discount, add a logo design item, due dates for first two milestones'")
    est2 = owner.tool("update_estimate", {
        "estimate_id": d["id"],
        "items": [
            {"description": "UX design — home, menu, gallery, contact (4 pages)", "quantity": 4, "unit_price": 35000},
            {"description": "Development with reservation system", "quantity": 1, "unit_price": 300000},
            {"description": "Content upload and SEO setup", "quantity": 1, "unit_price": 45000},
            {"description": "Logo design", "quantity": 1, "unit_price": 25000},
            {"description": "Annual hosting and maintenance", "quantity": 1, "unit_price": 96000, "is_addon": True},
        ],
        "milestones": [
            {"name": "Advance", "percent": 30, "note": "On signing", "due_date": "2026-12-01"},
            {"name": "Design approval", "percent": 20, "due_date": "2026-12-15"},
            {"name": "Beta site", "percent": 20},
            {"name": "Launch", "percent": 20},
            {"name": "After 30 days", "percent": 10},
        ],
    })
    d2 = est2["document"]
    # 140000 + 300000 + 45000 + 25000 = 510000; VAT 91800; total 601800
    assert d2["totals"]["total"] == "601800.00", d2["totals"]
    assert d2["status"] == "draft" and d2["id"] == d["id"]
    ok(f"updated draft: total LKR {d2['totals']['total']}; same estimate, still a draft")
    results["links"]["preview_v2"] = est2["preview_url"]

    step("4b. 'Make the estimate look more modern' — AI edits the template through MCP")
    guide = owner.rpc("tools/call", {"name": "get_template_guide", "arguments": {}})["content"][0]["text"]
    assert "data.json" in guide
    fonts = [f["family"] for f in owner.tool("list_fonts")["fonts"]]
    assert "Poppins" in fonts
    ok(f"AI reads the template guide and fonts ({', '.join(fonts)})")
    src = owner.tool("get_template", {"id": default_tpl["id"]})["typst_source"]
    broken = src + "\n#data.this-does-not-exist"
    err = owner.tool("create_template", {"name": f"Modern {RUN}", "doc_type": "estimate", "typst_source": broken}, expect_error=True)
    assert "does not compile" in err
    ok(f"broken template rejected with Typst's error for the AI to fix: {err.splitlines()[0][:90]}…")
    modern = src.replace('#let accent = rgb("#1e3a5f")', '#let accent = rgb("#0f766e")').replace('#let soft = rgb("#f5f7fa")', '#let soft = rgb("#ecfdf5")')
    assert modern != src
    t = owner.tool("create_template", {"name": f"Modern {RUN}", "doc_type": "estimate", "typst_source": modern})
    results["links"]["template_preview"] = t["preview_url"]
    ok(f"new template '{t['template']['name']}' v{t['template']['current_version']} compiled; sample preview link returned")
    est3 = owner.tool("update_estimate", {"estimate_id": d["id"], "template_id": t["template"]["id"]})
    assert est3["document"]["template"]["id"] == t["template"]["id"]
    results["links"]["preview_v3_modern"] = est3["preview_url"]
    ok("estimate switched to the new template; fresh preview link")

    step("Guard: the AI cannot finalise without the owner's confirmation")
    msg = owner.tool("finalise_document", {"kind": "estimate", "id": d["id"], "owner_confirmed": False}, expect_error=True)
    assert "owner" in msg.lower()
    still = owner.tool("get_estimate", {"id": d["id"]})["document"]
    assert still["status"] == "draft" and still["number"] is None
    ok("refused, and the estimate is still an unnumbered draft")

    step("5. Owner: 'Looks good, finalise it' — then downloads the PDF")
    f = owner.tool("finalise_document", {"kind": "estimate", "id": d["id"], "owner_confirmed": True})
    fd = f["document"]
    assert fd["status"] == "final" and re.fullmatch(r"EST-\d{4}-\d{4}", fd["number"]), fd["number"]
    assert fd["totals"]["total"] == "601800.00" and fd["valid_until"]
    ok(f"issued {fd['number']}, valid until {fd['valid_until']}, total LKR {fd['totals']['total']}")
    st, ct, pdf = fetch(f["pdf_url"].replace("http://localhost:3000", BASE))
    assert ct == "application/pdf" and pdf[:4] == b"%PDF"
    fonts_in_pdf = set(re.findall(rb"/BaseFont\s*/[A-Z]{6}\+([A-Za-z-]+)", pdf))
    assert any(f.startswith(b"Poppins") for f in fonts_in_pdf), fonts_in_pdf
    assert b"DRAFT" not in pdf
    ok(f"PDF downloaded: {len(pdf)//1024} KB, fonts {sorted(x.decode() for x in fonts_in_pdf)}, no DRAFT mark")
    pdf2 = fetch(f["pdf_url"].replace("http://localhost:3000", BASE))[2]
    assert hashlib.sha256(pdf).digest() == hashlib.sha256(pdf2).digest()
    ok("the stored PDF is served byte-identical on every download")
    results["links"]["final_pdf"] = f["pdf_url"]
    results["links"]["final_preview"] = f["preview_url"]
    with open(os.path.join(os.path.dirname(__file__), ".last-estimate.pdf"), "wb") as fh:
        fh.write(pdf)

    step("Locked: chat edits after finalising need a revision")
    msg = owner.tool("update_estimate", {"estimate_id": d["id"], "title": "changed"}, expect_error=True)
    assert "revision" in msg
    ok("update refused with a hint to revise")

    step("Later: 'Invoice the advance' — convert the first milestone and issue it")
    conv = owner.tool("convert_estimate_to_invoices", {"estimate_id": d["id"], "mode": "milestones",
                                                         "milestone_ids": [fd["milestones"][0]["id"]]})
    assert len(conv["invoices"]) == 1, len(conv["invoices"])
    inv = conv["invoices"][0]["document"]
    assert inv["totals"]["total"] == fd["milestones"][0]["amount"] == "180540.00", (inv["totals"], fd["milestones"][0])
    ok(f"one draft invoice for the advance: LKR {inv['totals']['total']} (30%), due {inv.get('due_date')}")
    fi = owner.tool("finalise_document", {"kind": "invoice", "id": inv["id"], "owner_confirmed": True})
    assert re.fullmatch(r"INV-\d{4}-\d{4}", fi["document"]["number"])
    assert fi["document"]["estimate_number"] == fd["number"]
    ok(f"issued {fi['document']['number']} (ref. {fd['number']})")
    again = owner.tool("get_estimate", {"id": d["id"]})["document"]
    billed = [m for m in again["milestones"] if m["invoice_id"]]
    assert len(billed) == 1 and billed[0]["invoice_number"] == fi["document"]["number"]
    ok("estimate shows milestone 1 billed; the other four still open")

    step("Audit: every AI action is recorded")
    sess = owner  # reuse the owner's web session cookie
    st, _, b = sess.req("GET", f"/api/audit?entity=estimate&entity_id={d['id']}&limit=50")
    entries = json.loads(b)
    actions = [e["action"] for e in entries if e["via"] == "ai"]
    for a in ("create", "update", "finalise", "convert"):
        assert a in actions, (a, actions)
    assert all(e["oauth_client_id"] for e in entries if e["via"] == "ai")
    ok(f"{len(entries)} audit entries for this estimate, AI ones tagged with the connected app")

    step("Isolation: another business's AI cannot see this data")
    other = Client(f"Other AI {RUN}", os.environ["OTHER_EMAIL"], os.environ["OTHER_PASSWORD"])
    other.connect()
    assert other.tool("find_customers", {"query": "Sunil"})["count"] == 0
    msg = other.tool("get_estimate", {"id": d["id"]}, expect_error=True)
    assert "not found" in msg
    assert other.tool("list_estimates", {})["count"] == 0 or all(e["id"] != d["id"] for e in other.tool("list_estimates", {})["estimates"])
    msg = other.tool("create_estimate", {"customer_id": cust["id"], "items": [{"description": "x", "quantity": 1, "unit_price": 1}]}, expect_error=True)
    ok("other tenant: customer invisible, estimate 'not found', cannot bill our customer")

    with open(os.path.join(os.path.dirname(__file__), ".last-run.json"), "w") as fh:
        json.dump(results, fh, indent=2)
    print("\nALL CORE-FLOW CHECKS PASSED")


if __name__ == "__main__":
    try:
        main()
    except (AssertionError, KeyError) as e:
        print(f"\n✗ FAILED: {e!r}")
        sys.exit(1)
