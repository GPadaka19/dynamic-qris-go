// ========================================
// Dynamic QRIS Converter — Frontend Logic
// ========================================

(function () {
  "use strict";

  // DOM Elements
  const form = document.getElementById("convertForm");
  const qrisInput = document.getElementById("qrisInput");
  const amountInput = document.getElementById("amountInput");
  const convertBtn = document.getElementById("convertBtn");
  const btnText = convertBtn.querySelector(".btn-text");
  const btnLoader = convertBtn.querySelector(".btn-loader");
  const btnIcon = convertBtn.querySelector(".btn-icon");
  const errorMessage = document.getElementById("errorMessage");
  const errorText = document.getElementById("errorText");
  const resultSection = document.getElementById("resultSection");
  const qrImage = document.getElementById("qrImage");
  const qrisOutput = document.getElementById("qrisOutput");
  const amountBadge = document.getElementById("amountBadge");
  const downloadBtn = document.getElementById("downloadBtn");
  const copyBtn = document.getElementById("copyBtn");
  const qrisSource = document.getElementById("qrisSource");

  // ========== Auto-Load QRIS ==========

  async function loadQRIS() {
    try {
      const response = await fetch("/api/qris");
      const data = await response.json();

      if (data.success && data.qris) {
        qrisInput.value = data.qris;
        qrisSource.textContent = "✅ Loaded dari data/qris.jpg";
        qrisSource.classList.add("source-loaded");
        // Focus amount input since QRIS is ready
        amountInput.focus();
      } else {
        qrisSource.textContent = "❌ Gagal memuat";
        qrisSource.classList.add("source-error");
        showError(data.error || "QRIS tidak ditemukan. Taruh file di data/qris.jpg");
      }
    } catch (err) {
      qrisSource.textContent = "❌ Server offline";
      qrisSource.classList.add("source-error");
      console.error("Failed to load QRIS:", err);
    }
  }

  // Load on page init
  loadQRIS();

  // ========== Amount Formatting ==========

  function formatNumberInput(value) {
    // Remove all non-digit characters
    const digits = value.replace(/\D/g, "");
    // Add thousand separators with dots
    return digits.replace(/\B(?=(\d{3})+(?!\d))/g, ".");
  }

  function parseAmount(value) {
    return parseInt(value.replace(/\D/g, ""), 10) || 0;
  }

  function formatRupiah(amount) {
    return "Rp " + amount.toString().replace(/\B(?=(\d{3})+(?!\d))/g, ".");
  }

  amountInput.addEventListener("input", function () {
    const cursorPos = this.selectionStart;
    const oldLength = this.value.length;
    this.value = formatNumberInput(this.value);
    const newLength = this.value.length;

    // Adjust cursor position after formatting
    const diff = newLength - oldLength;
    this.setSelectionRange(cursorPos + diff, cursorPos + diff);
  });

  // ========== Form Submission ==========

  form.addEventListener("submit", async function (e) {
    e.preventDefault();
    hideError();
    hideResult();

    const qrisValue = qrisInput.value.trim();
    const amountValue = parseAmount(amountInput.value);

    // Validation
    if (!qrisValue) {
      showError("QRIS belum dimuat. Pastikan file data/qris.jpg tersedia.");
      return;
    }

    if (amountValue <= 0) {
      showError("Nominal harus lebih dari 0.");
      return;
    }

    // Set loading state
    setLoading(true);

    try {
      const response = await fetch("/api/convert", {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({
          qris: qrisValue,
          amount: amountValue,
        }),
      });

      const data = await response.json();

      if (!data.success) {
        showError(data.error || "Terjadi kesalahan saat konversi.");
        return;
      }

      // Show result
      showResult(data.dynamic_qris, data.qr_image, amountValue);
    } catch (err) {
      showError("Gagal terhubung ke server. Pastikan server berjalan.");
      console.error("Fetch error:", err);
    } finally {
      setLoading(false);
    }
  });

  // ========== UI Helpers ==========

  function setLoading(loading) {
    convertBtn.disabled = loading;
    btnText.style.display = loading ? "none" : "";
    btnIcon.style.display = loading ? "none" : "";
    btnLoader.style.display = loading ? "flex" : "none";
  }

  function showError(message) {
    errorText.textContent = message;
    errorMessage.style.display = "flex";
  }

  function hideError() {
    errorMessage.style.display = "none";
  }

  function showResult(qrisString, qrImageSrc, amount) {
    qrImage.src = qrImageSrc;
    qrisOutput.textContent = qrisString;
    amountBadge.textContent = formatRupiah(amount);
    resultSection.style.display = "block";

    // Smooth scroll to result
    setTimeout(() => {
      resultSection.scrollIntoView({ behavior: "smooth", block: "center" });
    }, 100);
  }

  function hideResult() {
    resultSection.style.display = "none";
  }

  // ========== Download QR ==========

  downloadBtn.addEventListener("click", function () {
    const img = qrImage;
    if (!img.src) return;

    const canvas = document.createElement("canvas");
    const ctx = canvas.getContext("2d");

    // Draw with padding and branding
    const padding = 40;
    const badgeHeight = 50;
    const totalWidth = img.naturalWidth + padding * 2;
    const totalHeight = img.naturalHeight + padding * 2 + badgeHeight;

    canvas.width = totalWidth;
    canvas.height = totalHeight;

    // Background
    ctx.fillStyle = "#ffffff";
    ctx.roundRect(0, 0, totalWidth, totalHeight, 16);
    ctx.fill();

    // QR Code
    ctx.drawImage(img, padding, padding, img.naturalWidth, img.naturalHeight);

    // Amount text
    ctx.fillStyle = "#6366f1";
    ctx.font = "bold 20px Inter, sans-serif";
    ctx.textAlign = "center";
    const amountText = amountBadge.textContent;
    ctx.fillText(
      amountText,
      totalWidth / 2,
      img.naturalHeight + padding + badgeHeight - 10,
    );

    // Download
    const link = document.createElement("a");
    link.download = "qris-dynamic-" + parseAmount(amountInput.value) + ".png";
    link.href = canvas.toDataURL("image/png");
    link.click();
  });

  // ========== Copy QRIS String ==========

  copyBtn.addEventListener("click", async function () {
    const text = qrisOutput.textContent;
    if (!text) return;

    try {
      await navigator.clipboard.writeText(text);

      const originalHTML = this.innerHTML;
      this.innerHTML = `
                <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
                    <polyline points="20 6 9 17 4 12"/>
                </svg>
                Tersalin!
            `;
      this.style.borderColor = "rgba(34, 197, 94, 0.4)";
      this.style.color = "#22c55e";

      setTimeout(() => {
        this.innerHTML = originalHTML;
        this.style.borderColor = "";
        this.style.color = "";
      }, 2000);
    } catch (err) {
      // Fallback for older browsers
      const textarea = document.createElement("textarea");
      textarea.value = text;
      textarea.style.position = "fixed";
      textarea.style.opacity = "0";
      document.body.appendChild(textarea);
      textarea.select();
      document.execCommand("copy");
      document.body.removeChild(textarea);
    }
  });

  // ========== Keyboard Shortcuts ==========

  document.addEventListener("keydown", function (e) {
    // Ctrl/Cmd + Enter to submit
    if ((e.ctrlKey || e.metaKey) && e.key === "Enter") {
      e.preventDefault();
      form.dispatchEvent(new Event("submit"));
    }
  });
})();
