// ========================================
// Dynamic QRIS Converter — Frontend Logic
// ========================================

(function () {
  "use strict";

  // WA API calls are proxied through our Go backend (credentials stay server-side)
  const API_CONTACTS = "/api/contacts";
  const API_SEND     = "/api/send";

  // ─── QRIS Form Elements ───────────────────────────────────────
  const form         = document.getElementById("convertForm");
  const amountInput  = document.getElementById("amountInput");
  const remarksInput = document.getElementById("remarksInput");
  const convertBtn   = document.getElementById("convertBtn");
  const btnText      = convertBtn.querySelector(".btn-text");
  const btnLoader    = convertBtn.querySelector(".btn-loader");
  const btnArrow     = convertBtn.querySelector(".btn-arrow");
  const statusDot    = document.getElementById("statusDot");
  const statusText   = document.getElementById("statusText");
  const errorBanner  = document.getElementById("errorMessage");
  const errorText    = document.getElementById("errorText");

  const resultSection    = document.getElementById("resultSection");
  const resultActions    = document.getElementById("resultActions");
  const qrisStringDetail = document.getElementById("qrisStringDetail");
  const qrImage          = document.getElementById("qrImage");
  const merchantName     = document.getElementById("merchantName");
  const amountDisplay    = document.getElementById("amountDisplay");
  const remarksDisplay   = document.getElementById("remarksDisplay");
  const qrisOutput       = document.getElementById("qrisOutput");
  const downloadBtn      = document.getElementById("downloadBtn");
  const sendBtn          = document.getElementById("sendBtn");

  // ─── Send Modal Elements ──────────────────────────────────────
  const sendModal          = document.getElementById("sendModal");
  const modalCloseBtn      = document.getElementById("modalCloseBtn");
  const contactSearch      = document.getElementById("contactSearch");
  const contactList        = document.getElementById("contactList");
  const statePrompt        = document.getElementById("statePrompt");
  const stateLoading       = document.getElementById("stateLoading");
  const stateError         = document.getElementById("stateError");
  const stateErrorMsg      = document.getElementById("stateErrorMsg");
  const stateNoResults     = document.getElementById("stateNoResults");
  const confirmSendBtn     = document.getElementById("confirmSendBtn");
  const confirmBtnText     = document.getElementById("confirmBtnText");
  const confirmBtnLoader   = document.getElementById("confirmBtnLoader");
  const selectedContactInfo = document.getElementById("selectedContactInfo");
  const selectedAvatar     = document.getElementById("selectedAvatar");
  const selectedName       = document.getElementById("selectedName");
  const selectedPhone      = document.getElementById("selectedPhone");

  // ─── State ────────────────────────────────────────────────────
  let cachedQRIS    = null;
  let allContacts   = null; // null = not loaded yet, [] = loaded (may be empty)
  let selectedContact = null;

  // ═══════════════════════════════════════════════════════════════
  // QRIS Load & Form
  // ═══════════════════════════════════════════════════════════════

  async function loadQRIS() {
    try {
      const res  = await fetch("/api/qris");
      const data = await res.json();
      if (data.success && data.qris) {
        setStatus("loaded", "QRIS siap");
        amountInput.focus();
        return data.qris;
      }
      setStatus("error", "Gagal memuat QRIS");
      showError(data.error || "File data/qris.jpg tidak ditemukan.");
    } catch {
      setStatus("error", "Server offline");
    }
    return null;
  }

  loadQRIS().then(q => { cachedQRIS = q; });

  function setStatus(state, text) {
    statusDot.className  = "status-dot "  + state;
    statusText.className = "status-text " + state;
    statusText.textContent = text;
  }

  // ─── Amount Formatting ────────────────────────────────────────

  function formatThousands(v) {
    return v.replace(/\D/g, "").replace(/\B(?=(\d{3})+(?!\d))/g, ".");
  }

  function parseAmount(v) {
    return parseInt(v.replace(/\D/g, ""), 10) || 0;
  }

  function formatRupiah(n) {
    return "Rp " + n.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ".");
  }

  amountInput.addEventListener("input", function () {
    const pos    = this.selectionStart;
    const before = this.value.length;
    this.value   = formatThousands(this.value);
    const diff   = this.value.length - before;
    this.setSelectionRange(pos + diff, pos + diff);
  });

  // ─── Form Submit ──────────────────────────────────────────────

  form.addEventListener("submit", async function (e) {
    e.preventDefault();
    hideError();
    hideResult();

    if (!cachedQRIS) cachedQRIS = await loadQRIS();
    if (!cachedQRIS) {
      showError("QRIS belum tersedia. Pastikan file data/qris.jpg ada.");
      return;
    }

    const amount = parseAmount(amountInput.value);
    if (amount <= 0) {
      showError("Masukkan nominal yang valid (lebih dari 0).");
      return;
    }

    setLoading(true);

    try {
      const res  = await fetch("/api/convert", {
        method:  "POST",
        headers: { "Content-Type": "application/json" },
        body:    JSON.stringify({
          qris:    cachedQRIS,
          amount:  amount,
          remarks: remarksInput.value.trim(),
        }),
      });

      const data = await res.json();
      if (!data.success) { showError(data.error || "Konversi gagal."); return; }
      showResult(data);
    } catch {
      showError("Gagal terhubung ke server.");
    } finally {
      setLoading(false);
    }
  });

  // ─── UI Helpers ───────────────────────────────────────────────

  function setLoading(on) {
    convertBtn.disabled      = on;
    btnText.style.display    = on ? "none" : "";
    btnArrow.style.display   = on ? "none" : "";
    btnLoader.style.display  = on ? "flex" : "none";
  }

  function showError(msg) {
    errorText.textContent      = msg;
    errorBanner.style.display  = "flex";
  }

  function hideError() { errorBanner.style.display = "none"; }

  function showResult(data) {
    qrImage.src                   = data.qr_image;
    qrisOutput.textContent        = data.dynamic_qris;
    merchantName.textContent      = data.merchant_name || "—";
    amountDisplay.textContent     = formatRupiah(parseAmount(amountInput.value));
    remarksDisplay.textContent    = data.remarks || "";
    remarksDisplay.style.display  = data.remarks ? "block" : "none";

    resultSection.style.display    = "block";
    resultActions.style.display    = "flex";
    qrisStringDetail.style.display = "block";

    // Reset send modal state when a new QRIS is generated
    selectedContact = null;

    setTimeout(() => resultSection.scrollIntoView({ behavior: "smooth", block: "nearest" }), 80);
  }

  function hideResult() {
    resultSection.style.display    = "none";
    resultActions.style.display    = "none";
    qrisStringDetail.style.display = "none";
  }

  // ═══════════════════════════════════════════════════════════════
  // Download
  // ═══════════════════════════════════════════════════════════════

  downloadBtn.addEventListener("click", function () {
    if (!qrImage.src) return;

    const padding = 32;
    const qrSize  = qrImage.naturalWidth || 512;
    const textH   = 80;
    const cardW   = qrSize + padding * 2;
    const cardH   = qrSize + padding * 2 + textH;

    const canvas = document.createElement("canvas");
    canvas.width  = cardW;
    canvas.height = cardH;
    const ctx = canvas.getContext("2d");

    ctx.fillStyle = "#ffffff";
    ctx.roundRect(0, 0, cardW, cardH, 16);
    ctx.fill();

    ctx.drawImage(qrImage, padding, padding, qrSize, qrSize);

    ctx.fillStyle   = "#0f1623";
    ctx.font        = `bold ${Math.round(qrSize * 0.055)}px Inter, sans-serif`;
    ctx.textAlign   = "center";
    ctx.fillText(amountDisplay.textContent, cardW / 2, qrSize + padding + 44);

    if (remarksDisplay.textContent) {
      ctx.fillStyle = "#6b7280";
      ctx.font      = `${Math.round(qrSize * 0.04)}px Inter, sans-serif`;
      ctx.fillText(remarksDisplay.textContent, cardW / 2, qrSize + padding + 70);
    }

    const link    = document.createElement("a");
    link.download = `qris-${parseAmount(amountInput.value)}.png`;
    link.href     = canvas.toDataURL("image/png");
    link.click();
  });

  // ═══════════════════════════════════════════════════════════════
  // Send Modal — Open / Close
  // ═══════════════════════════════════════════════════════════════

  sendBtn.addEventListener("click", openSendModal);
  modalCloseBtn.addEventListener("click", closeSendModal);

  sendModal.addEventListener("click", function (e) {
    if (e.target === sendModal) closeSendModal();
  });

  document.addEventListener("keydown", function (e) {
    if (e.key === "Escape" && sendModal.style.display !== "none") closeSendModal();
    if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
      e.preventDefault();
      form.dispatchEvent(new Event("submit"));
    }
  });

  function openSendModal() {
    sendModal.style.display = "flex";
    document.body.style.overflow = "hidden";
    resetSendModal();
    contactSearch.focus();
    fetchContacts(); // hit endpoint immediately, cache result
  }

  function closeSendModal() {
    sendModal.style.display = "none";
    document.body.style.overflow = "";
  }

  // ═══════════════════════════════════════════════════════════════
  // Contacts — Fetch & Filter
  // ═══════════════════════════════════════════════════════════════

  async function fetchContacts() {
    if (allContacts !== null) return; // already fetched

    setContactState("loading");

    try {
      const res  = await fetch(API_CONTACTS);
      const data = await res.json();

      if (data.code !== "SUCCESS") {
        throw new Error(data.message || "Gagal memuat kontak");
      }

      allContacts = (data.results?.data || []).filter(c => c.jid && c.name);
      // Re-render with whatever is currently in the search box
      renderContacts(contactSearch.value.trim());
    } catch (err) {
      stateErrorMsg.textContent = err.message || "Gagal memuat kontak";
      setContactState("error");
    }
  }

  contactSearch.addEventListener("input", function () {
    renderContacts(this.value.trim());
  });

  function renderContacts(query) {
    contactList.innerHTML = "";

    // Contacts not yet loaded — don't change the loading/error state
    if (allContacts === null) return;

    if (!query) {
      setContactState("prompt");
      return;
    }

    const q       = query.toLowerCase();
    const matches = allContacts.filter(c => c.name.toLowerCase().includes(q));

    if (matches.length === 0) {
      setContactState("no-results");
      return;
    }

    setContactState("list");

    matches.forEach(contact => {
      const phone    = contact.jid.split("@")[0];
      const initials = getInitials(contact.name);
      const color    = avatarColor(contact.name);
      const isSelected = selectedContact?.jid === contact.jid;

      const item = document.createElement("div");
      item.className = "contact-item" + (isSelected ? " selected" : "");
      item.dataset.jid = contact.jid;
      item.innerHTML = `
        <div class="contact-avatar" style="background:${color}">${initials}</div>
        <div class="contact-info">
          <span class="contact-name">${escapeHtml(contact.name)}</span>
          <span class="contact-phone">+${phone}</span>
        </div>
        <div class="contact-check">
          <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
            <polyline points="20 6 9 17 4 12"/>
          </svg>
        </div>
      `;

      item.addEventListener("click", () => selectContact(contact));
      contactList.appendChild(item);
    });
  }

  function selectContact(contact) {
    selectedContact = contact;

    const phone    = contact.jid.split("@")[0];
    const color    = avatarColor(contact.name);
    const initials = getInitials(contact.name);

    // Update selected info bar
    selectedAvatar.style.background = color;
    selectedAvatar.textContent       = initials;
    selectedName.textContent         = contact.name;
    selectedPhone.textContent        = "+" + phone;
    selectedContactInfo.style.display = "flex";

    // Enable send button
    confirmBtnText.textContent = `Kirim ke ${contact.name}`;
    confirmSendBtn.disabled    = false;

    // Re-render list to update selection highlight
    renderContacts(contactSearch.value.trim());
  }

  function setContactState(state) {
    statePrompt.style.display    = state === "prompt"     ? "flex" : "none";
    stateLoading.style.display   = state === "loading"    ? "flex" : "none";
    stateError.style.display     = state === "error"      ? "flex" : "none";
    stateNoResults.style.display = state === "no-results" ? "flex" : "none";
    // "list" = nothing special, contactList renders itself
  }

  // ═══════════════════════════════════════════════════════════════
  // Send Image
  // ═══════════════════════════════════════════════════════════════

  confirmSendBtn.addEventListener("click", async function () {
    if (!selectedContact || !qrImage.src) return;

    setSendLoading(true);

    try {
      const phone = selectedContact.jid.split("@")[0];

      const res  = await fetch(API_SEND, {
        method:  "POST",
        headers: { "Content-Type": "application/json" },
        body:    JSON.stringify({
          phone:   phone,
          caption: buildCaption(),
          image:   qrImage.src, // base64 data URL — backend decodes and forwards as binary
        }),
      });
      const data = await res.json();

      if (!res.ok) throw new Error(data.message || "Pengiriman gagal");

      showSendSuccess(selectedContact.name);
    } catch (err) {
      stateErrorMsg.textContent = err.message || "Gagal mengirim QRIS";
      setContactState("error");
      setSendLoading(false);
    }
  });

  function buildCaption() {
    let caption = amountDisplay.textContent;
    if (remarksDisplay.textContent) caption += "\n" + remarksDisplay.textContent;
    return caption;
  }

  function setSendLoading(on) {
    confirmSendBtn.disabled         = on;
    confirmBtnText.style.display    = on ? "none" : "";
    confirmBtnLoader.style.display  = on ? "flex" : "none";
  }

  function showSendSuccess(name) {
    const overlay = document.getElementById("sendSuccessOverlay");
    document.getElementById("successRecipient").textContent = "ke " + name;
    overlay.style.display = "flex";

    setTimeout(() => {
      closeSendModal();
      overlay.style.display = "none";
      resetSendModal();
    }, 2000);
  }

  function resetSendModal() {
    selectedContact                   = null;
    contactSearch.value               = "";
    contactList.innerHTML             = "";
    selectedContactInfo.style.display = "none";
    confirmSendBtn.disabled           = true;
    confirmBtnText.textContent        = "Pilih kontak dahulu";
    setSendLoading(false);
    setContactState("prompt");
  }

  // ═══════════════════════════════════════════════════════════════
  // Utilities
  // ═══════════════════════════════════════════════════════════════

  function getInitials(name) {
    return name.trim().split(/\s+/).slice(0, 2).map(w => w[0] || "").join("").toUpperCase();
  }

  const AVATAR_COLORS = ["#0b2562","#15803d","#b45309","#6d28d9","#b91c1c","#0369a1","#9d174d"];
  function avatarColor(name) {
    let hash = 0;
    for (let i = 0; i < name.length; i++) hash = (hash * 31 + name.charCodeAt(i)) | 0;
    return AVATAR_COLORS[Math.abs(hash) % AVATAR_COLORS.length];
  }

  function escapeHtml(str) {
    return str.replace(/&/g,"&amp;").replace(/</g,"&lt;").replace(/>/g,"&gt;").replace(/"/g,"&quot;");
  }
})();
