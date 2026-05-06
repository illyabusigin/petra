(() => {
  // Petra placeholders are substituted by hot_reload.go before this file is served.
  if (window.__petraHotReload) return;
  window.__petraHotReload = true;

  const currentScript = document.currentScript;
  const clientScriptPath = __PETRA_CLIENT_SCRIPT_PATH__;
  const socketPath = __PETRA_SOCKET_PATH__;
  const scriptURL = new URL(currentScript ? currentScript.src : __PETRA_CLIENT_SCRIPT_FALLBACK__, window.location.href);
  if (scriptURL.pathname.endsWith(clientScriptPath)) {
    scriptURL.pathname = scriptURL.pathname.slice(0, -clientScriptPath.length) + socketPath;
  } else {
    scriptURL.pathname = socketPath;
  }
  scriptURL.protocol = window.location.protocol === "https:" ? "wss:" : "ws:";

  let socket;
  let reconnectTimer;
  let disconnectedNoticeTimer;
  let reconnectAttempts = 0;
  let connectedOnce = false;
  let disconnectedSince = 0;

  const reconnectBaseDelay = __PETRA_RECONNECT_BASE_DELAY__;
  const reconnectMaxDelay = __PETRA_RECONNECT_MAX_DELAY__;
  const disconnectNoticeDelay = 2000;

  function showOverlay(payload) {
    let overlay = document.getElementById("petra-reload-error");
    if (!overlay) {
      overlay = document.createElement("div");
      overlay.id = "petra-reload-error";
      overlay.setAttribute("role", "alert");
      overlay.style.cssText = [
        "position:fixed",
        "inset:16px",
        "z-index:2147483647",
        "max-width:1040px",
        "max-height:calc(100vh - 32px)",
        "overflow:auto",
        "background:#111113",
        "color:#fff",
        "border:1px solid #fb7185",
        "box-shadow:0 24px 80px rgba(0,0,0,.35)",
        "border-radius:8px",
        "font:14px/1.5 ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif"
      ].join(";");
      document.body.appendChild(overlay);
    }

    const debug = payload.debug || {};
    const changedPaths = Array.isArray(debug.changed_paths) && debug.changed_paths.length ? debug.changed_paths : payload.paths;
    const frames = Array.isArray(debug.frames) ? debug.frames : [];
    overlay.replaceChildren();

    const shell = document.createElement("div");
    shell.style.cssText = "padding:16px";
    overlay.appendChild(shell);

    const eyebrow = document.createElement("div");
    eyebrow.textContent = "Petra hot reload";
    eyebrow.style.cssText = "color:#fda4af;font-size:12px;font-weight:700;letter-spacing:.08em;text-transform:uppercase";
    shell.appendChild(eyebrow);

    const title = document.createElement("div");
    title.textContent = "Template reload failed";
    title.style.cssText = "margin-top:4px;font-size:24px;font-weight:800;letter-spacing:0";
    shell.appendChild(title);

    const message = document.createElement("pre");
    message.textContent = payload.message || debug.message || "Unknown reload error";
    message.style.cssText = "margin:12px 0 0;padding:12px;background:#09090b;border:1px solid #3f3f46;border-radius:8px;color:#f4f4f5;white-space:pre-wrap;word-break:break-word;font:13px/1.55 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace";
    shell.appendChild(message);

    const meta = document.createElement("div");
    meta.style.cssText = "display:grid;grid-template-columns:repeat(auto-fit,minmax(160px,1fr));gap:8px;margin-top:12px";
    shell.appendChild(meta);

    function addMeta(label, value) {
      if (!value) return;
      const item = document.createElement("div");
      item.style.cssText = "padding:10px;background:#18181b;border:1px solid #3f3f46;border-radius:8px;min-width:0";
      const key = document.createElement("div");
      key.textContent = label;
      key.style.cssText = "color:#a1a1aa;font-size:11px;text-transform:uppercase;letter-spacing:.06em";
      const val = document.createElement("div");
      val.textContent = value;
      val.style.cssText = "margin-top:3px;overflow-wrap:anywhere;font:13px/1.45 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace";
      item.append(key, val);
      meta.appendChild(item);
    }

    addMeta("Kind", debug.kind);
    addMeta("Operation", debug.operation);
    addMeta("Role", debug.dependency_role);
    addMeta("Page", debug.page);
    addMeta("Component", debug.component);
    addMeta("Layout", debug.layout);
    addMeta("Path", debug.path);
    addMeta("Fallback", debug.fallback_reason);
    if (debug.location) {
      const loc = debug.location.template + (debug.location.line ? ":" + debug.location.line : "") + (debug.location.column ? ":" + debug.location.column : "");
      addMeta("Location", loc);
    }

    function addSection(label, lines) {
      if (!Array.isArray(lines) || lines.length === 0) return;
      const section = document.createElement("section");
      section.style.cssText = "margin-top:12px;border:1px solid #3f3f46;border-radius:8px;overflow:hidden;background:#18181b";
      const heading = document.createElement("div");
      heading.textContent = label;
      heading.style.cssText = "padding:9px 11px;background:#202024;border-bottom:1px solid #3f3f46;font-weight:700";
      const body = document.createElement("pre");
      body.textContent = lines.join("\n");
      body.style.cssText = "margin:0;padding:12px;overflow:auto;background:#09090b;color:#f4f4f5;font:13px/1.55 ui-monospace,SFMono-Regular,Menlo,Monaco,Consolas,monospace";
      section.append(heading, body);
      shell.appendChild(section);
    }

    addSection("Changed paths", changedPaths);
    addSection("Affected pages", debug.affected_pages);
    if (debug.source && Array.isArray(debug.source.lines) && debug.source.lines.length) {
      const sourceTitle = "Source excerpt " + (debug.source.path || "") + (debug.source.line ? ":" + debug.source.line : "") + (debug.source.column ? ":" + debug.source.column : "");
      addSection(sourceTitle, debug.source.lines.map((line) => {
        const prefix = line.highlight ? "> " : "  ";
        return prefix + String(line.number).padStart(4, " ") + " | " + (line.text || "");
      }));
    }
    addSection("Parsed files", debug.files);
    addSection("Template stack", frames.map((frame) => {
      const parts = [];
      if (frame.kind) parts.push(frame.kind);
      if (frame.name) parts.push(frame.name);
      if (frame.path) parts.push(frame.path);
      return parts.join(" ");
    }));
    if (debug.go_stack) addSection("Go stack", [debug.go_stack]);
  }

  function showDisconnected(delay) {
    let overlay = document.getElementById("petra-reload-disconnected");
    if (!overlay) {
      overlay = document.createElement("div");
      overlay.id = "petra-reload-disconnected";
      overlay.setAttribute("role", "status");
      overlay.style.cssText = [
        "position:fixed",
        "right:16px",
        "bottom:16px",
        "z-index:2147483646",
        "max-width:360px",
        "background:#111827",
        "color:#f9fafb",
        "border:1px solid #374151",
        "box-shadow:0 16px 48px rgba(0,0,0,.28)",
        "border-radius:8px",
        "font:13px/1.45 ui-sans-serif,system-ui,-apple-system,BlinkMacSystemFont,Segoe UI,sans-serif",
        "padding:12px 14px"
      ].join(";");
      document.body.appendChild(overlay);
    }

    const seconds = Math.max(1, Math.round(delay / 1000));
    overlay.textContent = "Petra dev server disconnected. Retrying in " + seconds + "s.";
  }

  function removeOverlay() {
    const overlay = document.getElementById("petra-reload-error");
    if (overlay) overlay.remove();
  }

  function removeDisconnected() {
    const overlay = document.getElementById("petra-reload-disconnected");
    if (overlay) overlay.remove();
  }

  function scheduleDisconnectedNotice(retryAt) {
    window.clearTimeout(disconnectedNoticeTimer);
    const elapsed = Date.now() - disconnectedSince;
    const wait = Math.max(0, disconnectNoticeDelay - elapsed);

    disconnectedNoticeTimer = window.setTimeout(() => {
      if (!disconnectedSince) return;
      showDisconnected(Math.max(0, retryAt - Date.now()));
    }, wait);
  }

  function assetPath(value) {
    try {
      return new URL(value, window.location.href).pathname;
    } catch {
      return "";
    }
  }

  function reloadAssets(paths) {
    const links = Array.from(document.querySelectorAll('link[rel~="stylesheet"][href]'));
    if (links.length === 0) {
      window.location.reload();
      return;
    }

    const changed = Array.isArray(paths) ? new Set(paths.map(assetPath).filter(Boolean)) : new Set();
    const targets = changed.size === 0 ? links : links.filter((link) => changed.has(assetPath(link.href)));
    if (targets.length === 0) {
      window.location.reload();
      return;
    }

    const stamp = Date.now().toString();
    for (const link of targets) {
      const href = new URL(link.href, window.location.href);
      href.searchParams.set("_petra_reload", stamp);
      link.href = href.toString();
    }
  }

  function nextReconnectDelay() {
    const exponent = Math.min(reconnectAttempts, 5);
    const jitter = Math.floor(Math.random() * 250);
    return Math.min(reconnectMaxDelay, reconnectBaseDelay * 2 ** exponent) + jitter;
  }

  function scheduleReconnect() {
    window.clearTimeout(reconnectTimer);
    if (!disconnectedSince) {
      disconnectedSince = Date.now();
    }

    const delay = nextReconnectDelay();
    reconnectAttempts += 1;
    const retryAt = Date.now() + delay;

    if (connectedOnce || reconnectAttempts > 1) {
      scheduleDisconnectedNotice(retryAt);
    }

    reconnectTimer = window.setTimeout(connect, delay);
  }

  function connect() {
    try {
      socket = new WebSocket(scriptURL.toString());
    } catch {
      scheduleReconnect();
      return;
    }

    socket.addEventListener("open", () => {
      connectedOnce = true;
      reconnectAttempts = 0;
      disconnectedSince = 0;
      window.clearTimeout(reconnectTimer);
      window.clearTimeout(disconnectedNoticeTimer);
      removeDisconnected();
    });

    socket.addEventListener("message", (event) => {
      if (event.data === "reload") {
        removeOverlay();
        window.location.reload();
        return;
      }
      if (event.data === "reload_assets") {
        reloadAssets();
        return;
      }

      let payload;
      try {
        payload = JSON.parse(event.data);
      } catch {
        return;
      }

      if (payload.type === "reload_error") {
        showOverlay(payload);
      } else if (payload.type === "reload_ok") {
        removeOverlay();
      } else if (payload.type === "reload_assets") {
        reloadAssets(payload.paths);
      } else if (payload.type === "reload") {
        removeOverlay();
        window.location.reload();
      }
    });

    socket.addEventListener("close", () => {
      scheduleReconnect();
    });
  }

  window.addEventListener("beforeunload", () => {
    window.clearTimeout(reconnectTimer);
    window.clearTimeout(disconnectedNoticeTimer);
    if (socket && socket.readyState === WebSocket.OPEN) {
      socket.close();
    }
  });

  connect();
})();
