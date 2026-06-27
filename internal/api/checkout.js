window.ZiaCheckout = function (opts) {
  var token = opts.token;
  var baseURL = opts.baseURL || window.location.origin;
  var container = document.getElementById(opts.container || "zia-checkout");
  var pollInterval = opts.pollInterval || 2000;

  if (!token) {
    renderError("ZiaCheckout: token is required");
    return;
  }

  if (!container) {
    renderError("ZiaCheckout: container element not found");
    return;
  }

  var state = "loading";

  function render() {
    container.innerHTML =
      '<div style="max-width:400px;margin:0 auto;font-family:-apple-system,BlinkMacSystemFont,\'Segoe UI\',Roboto,sans-serif;padding:24px;border:1px solid #e2e8f0;border-radius:12px;box-shadow:0 1px 3px rgba(0,0,0,0.1)">' +
      '<h3 style="margin:0 0 16px;font-size:18px;font-weight:600;color:#1a202c">Complete Payment</h3>' +
      (state === "loading" ? loadingUI() : "") +
      (state === "requires_action" ? actionUI() : "") +
      (state === "processing" ? processingUI() : "") +
      (state === "succeeded" ? successUI() : "") +
      (state === "failed" ? errorUI() : "") +
      (state === "expired" ? expiredUI() : "") +
      "</div>";
  }

  function loadingUI() {
    return '<div style="text-align:center;padding:24px"><div style="border:3px solid #e2e8f0;border-top-color:#4299e1;border-radius:50%;width:32px;height:32px;animation:zia-spin 0.8s linear infinite;margin:0 auto"></div><p style="color:#718096;margin-top:12px">Loading checkout...</p></div>';
  }

  function actionUI() {
    return '<p style="color:#4a5568;margin-bottom:16px">Enter your M-Pesa PIN when prompted on your phone to complete the payment.</p><div style="background:#f7fafc;border:1px solid #e2e8f0;border-radius:8px;padding:16px;margin-bottom:16px"><p style="margin:0;font-size:14px;color:#718096">Check your phone for the STK Push prompt</p></div>';
  }

  function processingUI() {
    return '<div style="text-align:center;padding:24px"><div style="border:3px solid #e2e8f0;border-top-color:#4299e1;border-radius:50%;width:32px;height:32px;animation:zia-spin 0.8s linear infinite;margin:0 auto"></div><p style="color:#718096;margin-top:12px">Processing payment...</p></div>';
  }

  function successUI() {
    return '<div style="text-align:center;padding:16px"><div style="background:#48bb78;color:white;width:48px;height:48px;border-radius:50%;display:flex;align-items:center;justify-content:center;margin:0 auto;font-size:24px">✓</div><p style="color:#2f855a;font-weight:600;margin-top:12px">Payment Successful!</p></div>';
  }

  function errorUI() {
    return '<div style="text-align:center;padding:16px"><div style="background:#f56565;color:white;width:48px;height:48px;border-radius:50%;display:flex;align-items:center;justify-content:center;margin:0 auto;font-size:24px">✗</div><p style="color:#c53030;font-weight:600;margin-top:12px">Payment Failed</p></div>';
  }

  function expiredUI() {
    return '<div style="text-align:center;padding:16px"><p style="color:#718096">This checkout session has expired. Please start a new payment.</p></div>';
  }

  function renderError(msg) {
    if (container) {
      container.innerHTML = '<div style="color:#c53030;padding:12px;text-align:center">' + msg + "</div>";
    }
  }

  function poll() {
    fetch(baseURL + "/v1/checkout/" + encodeURIComponent(token))
      .then(function (res) { return res.json(); })
      .then(function (data) {
        var pdata = data.primaryData;
        if (!pdata) return;

        state = pdata.status;
        render();

        if (state === "succeeded" || state === "failed" || state === "expired") {
          if (opts.onComplete) opts.onComplete(state);
          return;
        }

        setTimeout(poll, pollInterval);
      })
      .catch(function () {
        setTimeout(poll, pollInterval);
      });
  }

  var style = document.createElement("style");
  style.textContent = "@keyframes zia-spin { to { transform: rotate(360deg) } }";
  document.head.appendChild(style);

  render();
  poll();
};
