// Thawr admin UI: plain JS against /api/v1. No build step by design.
(() => {
  "use strict";
  const $ = (sel) => document.querySelector(sel);
  let csrf = "";
  let me = null;

  async function api(method, path, body) {
    const headers = {};
    if (body !== undefined) headers["Content-Type"] = "application/json";
    if (method !== "GET") headers["X-CSRF-Token"] = csrf;
    const resp = await fetch(path, {
      method, headers, credentials: "same-origin", body: body === undefined ? undefined : JSON.stringify(body),
    });
    if (resp.status === 204) return null;
    const data = await resp.json().catch(() => ({}));
    if (!resp.ok) throw new Error(data.error || resp.statusText);
    return data;
  }

  function show(id, visible) { $(id).hidden = !visible; }

  function cell(text) { const td = document.createElement("td"); td.textContent = text ?? ""; return td; }

  function actionCell(label, onClick) {
    const td = document.createElement("td");
    const b = document.createElement("button");
    b.className = "link"; b.textContent = label; b.addEventListener("click", onClick);
    td.appendChild(b);
    return td;
  }

  async function loadPeers() {
    const peers = await api("GET", "/api/v1/peers");
    const tbody = $("#peers tbody");
    tbody.replaceChildren();
    for (const p of peers) {
      const tr = document.createElement("tr");
      tr.append(cell(p.name), cell(p.ipv4), cell(p.kind), cell(p.owner || "-"), cell(p.tags.join(", ") || "-"), cell(p.created_at));
      if (me.role === "admin") {
        const td = document.createElement("td");
        const rename = document.createElement("button");
        rename.className = "link"; rename.textContent = "Rename";
        rename.addEventListener("click", async () => {
          const name = prompt(`New name for ${p.name}`, p.name);
          if (!name || name === p.name) return;
          try { await api("PATCH", `/api/v1/peers/${encodeURIComponent(p.name)}`, { name }); await loadPeers(); }
          catch (e) { alert(e.message); }
        });
        const del = document.createElement("button");
        del.className = "link"; del.textContent = "Delete";
        del.addEventListener("click", async () => {
          if (!confirm(`Delete peer ${p.name}? Every client drops it within seconds.`)) return;
          try { await api("DELETE", `/api/v1/peers/${encodeURIComponent(p.name)}`); await loadPeers(); }
          catch (e) { alert(e.message); }
        });
        td.append(rename, del);
        tr.append(td);
      } else {
        tr.append(cell(""));
      }
      tbody.append(tr);
    }
  }

  async function loadTokens() {
    const tokens = await api("GET", "/api/v1/tokens");
    const tbody = $("#tokens tbody");
    tbody.replaceChildren();
    for (const t of tokens) {
      const tr = document.createElement("tr");
      tr.append(cell(t.id), cell(t.owner), cell(t.kind), cell(t.tags.join(", ") || "-"), cell(t.expires_at), cell(t.used_at ? "used" : "unused"));
      tr.append(actionCell("Revoke", async () => {
        try { await api("DELETE", `/api/v1/tokens/${encodeURIComponent(t.id)}`); await loadTokens(); }
        catch (e) { alert(e.message); }
      }));
      tbody.append(tr);
    }
  }

  async function loadUsers() {
    if (me.role !== "admin") { show("#users-section", false); return; }
    show("#users-section", true);
    const users = await api("GET", "/api/v1/users");
    const tbody = $("#users tbody");
    tbody.replaceChildren();
    for (const u of users) {
      const tr = document.createElement("tr");
      tr.append(cell(u.name), cell(u.role), cell(u.created_at));
      tbody.append(tr);
    }
  }

  async function refresh() {
    await Promise.all([loadPeers(), loadTokens(), loadUsers()]);
  }

  async function enter(session) {
    me = session; csrf = session.csrf || "";
    $("#who").textContent = `${me.name} (${me.role})`;
    show("#login", false); show("#app", true); show("#logout", true);
    $("#token-form").owner.value = $("#token-form").owner.value || me.name;
    await refresh();
  }

  function leave() {
    me = null; csrf = "";
    $("#who").textContent = "";
    show("#app", false); show("#logout", false); show("#login", true);
  }

  $("#login-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const f = ev.target;
    $("#login-error").textContent = "";
    try {
      const session = await api("POST", "/api/v1/login", { name: f.name.value, password: f.password.value });
      f.password.value = "";
      await enter(session);
    } catch (e) { $("#login-error").textContent = e.message; }
  });

  $("#logout").addEventListener("click", async () => { try { await api("POST", "/api/v1/logout"); } finally { leave(); } });
  $("#refresh").addEventListener("click", () => refresh().catch((e) => alert(e.message)));

  $("#token-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const f = ev.target;
    $("#token-error").textContent = "";
    const tags = f.tags.value.split(",").map((s) => s.trim()).filter(Boolean);
    try {
      const t = await api("POST", "/api/v1/tokens", {
        owner: f.owner.value, kind: f.kind.value, tags, peer_name: f.peer_name.value, expires: f.expires.value,
      });
      $("#join-command").textContent = t.join_command;
      show("#token-result", true);
      await loadTokens();
    } catch (e) { $("#token-error").textContent = e.message; }
  });
  $("#copy").addEventListener("click", () => navigator.clipboard?.writeText($("#join-command").textContent));
  $("#dismiss").addEventListener("click", () => { $("#join-command").textContent = ""; show("#token-result", false); });

  $("#user-form").addEventListener("submit", async (ev) => {
    ev.preventDefault();
    const f = ev.target;
    $("#user-error").textContent = "";
    try {
      await api("POST", "/api/v1/users", { name: f.name.value, role: f.role.value, password: f.password.value });
      f.reset();
      await loadUsers();
    } catch (e) { $("#user-error").textContent = e.message; }
  });

  // Resume an existing session, else show the login form.
  api("GET", "/api/v1/me").then(enter).catch(leave);
})();
