// adminHTML is the admin panel single-page app, rendered in the
// dreamTunnel design language (white paper, 1px hairlines, Manrope 400,
// no radii, no shadows, ↗ glyph, inverse hovers).
package main

const adminHTML = `<!DOCTYPE html>
<html lang="ru">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>dreamTunnel — панель управления</title>
<style>
:root{
  --ink:#000000; --paper:#ffffff; --graphite:#999999;
  --line:1px solid #000; --font:'Manrope',system-ui,sans-serif;
}
*{border-radius:0;margin:0;padding:0;box-sizing:border-box;font-synthesis:none;}
::selection{color:#fff;background:#000;}
body{font-family:var(--font);font-weight:400;background:var(--paper);color:var(--ink);font-size:16px;line-height:1.15;}
:focus-visible{outline:2px solid #000;outline-offset:3px;}
a{color:inherit;text-decoration:none;}

/* login screen */
.login-wrap{min-height:100vh;display:grid;place-items:center;padding:16px;}
.login-box{width:min(460px,100%);}
.login-box h1{font-size:clamp(44px,7vw,84px);line-height:.84;letter-spacing:-.06em;margin-bottom:35px;}
.login-box .sub{color:var(--graphite);font-size:14px;margin-bottom:26px;}
.login-box label{display:block;font-size:14px;color:var(--graphite);margin:22px 0 6px;}
.login-box input{width:100%;min-height:50px;border:var(--line);padding:12px;font:inherit;background:var(--paper);}
.login-box button{width:100%;min-height:50px;margin-top:30px;background:#000;color:#fff;border:var(--line);font:inherit;cursor:pointer;}
.login-box button:hover{background:#fff;color:#000;}
.login-box button:active{transform:translateY(1px);}
.login-err{margin-top:14px;font-size:14px;color:#c22222;border-top:var(--line);border-bottom:var(--line);padding:8px 0;}

/* app layout */
.topbar{height:52px;border-bottom:var(--line);display:flex;align-items:center;justify-content:space-between;padding:0 24px;position:sticky;top:0;background:var(--paper);z-index:10;}
.topbar .logo{letter-spacing:-.01em;}
.topbar .right{font-size:14px;display:flex;gap:18px;align-items:center;}
.topbar .right a{border-bottom:1px solid #000;cursor:pointer;}
.topbar .right a:hover{background:#000;color:#fff;}

.wrap{max-width:1240px;margin:0 auto;padding:34px 24px 90px;}
.page-heading{min-height:150px;border-bottom:var(--line);margin-bottom:40px;padding-bottom:26px;}
.page-heading .idx{color:var(--graphite);font-size:16px;margin-bottom:12px;}
.page-heading h2{font-size:clamp(42px,6vw,84px);line-height:.85;letter-spacing:-.055em;}
.page-heading .lead{margin-top:16px;font-size:18px;color:#555;max-width:60ch;}

/* stat cards */
.stats{display:grid;grid-template-columns:repeat(4,1fr);gap:20px;margin-bottom:50px;}
.stat{border:var(--line);padding:22px 18px;display:flex;flex-direction:column;gap:8px;}
.stat .v{font-size:clamp(30px,3.5vw,46px);letter-spacing:-.03em;line-height:1;}
.stat .l{color:var(--graphite);font-size:14px;}

/* sections */
.sec{margin-top:60px;}
.sec h3{font-size:clamp(26px,3vw,38px);letter-spacing:-.04em;line-height:.9;margin-bottom:24px;}
.sec h3 .cnt{color:var(--graphite);font-size:16px;letter-spacing:0;}

/* tables */
.table-wrap{overflow-x:auto;}
table{width:100%;border-collapse:collapse;text-align:left;}
th{color:var(--graphite);font-weight:400;padding:12px 12px 12px 0;border-bottom:var(--line);font-size:14px;}
td{padding:14px 12px 14px 0;border-bottom:var(--line);vertical-align:top;font-size:15px;}
tr:hover td{background:#f3f3f3;}
.mono{font-size:14px;}
.dim{color:var(--graphite);}
.badge{display:inline-block;border:var(--line);padding:2px 8px;font-size:13px;}
.badge.on{border-color:#0a7a2f;color:#0a7a2f;}
.badge.off{border-color:#b07000;color:#b07000;}
.badge.killed{border-color:#c22222;color:#c22222;}

/* actions */
button.act{min-height:36px;padding:6px 14px;border:var(--line);background:#fff;font:inherit;font-size:14px;cursor:pointer;}
button.act:hover{background:#000;color:#fff;}
button.act:active{transform:translateY(1px);}
button.act.danger{border-color:#c22222;color:#c22222;}
button.act.danger:hover{background:#c22222;color:#fff;}

/* ban form */
.ban-form{display:flex;gap:0;border:var(--line);max-width:420px;margin:24px 0 30px;}
.ban-form input{flex:1;border:none;min-height:48px;padding:12px;font:inherit;}
.ban-form input:focus{outline:none;}
.ban-form button{border:none;border-left:var(--line);background:#000;color:#fff;padding:12px 20px;font:inherit;cursor:pointer;}
.ban-form button:hover{background:#fff;color:#000;}

/* message strip (no alerts) */
.strip{display:none;border-top:var(--line);border-bottom:var(--line);padding:10px 0;margin:18px 0;font-size:14px;}
.strip.err{display:block;color:#c22222;}
.strip.ok{display:block;}

@media(max-width:800px){
  .stats{grid-template-columns:1fr 1fr;}
  .wrap{padding:24px 14px 70px;}
}
</style>
</head>
<body>

<div id="login" class="login-wrap" style="display:none">
  <div class="login-box">
    <div class="idx">dreamTunnel / доступ</div>
    <h1>Панель управления</h1>
    <div class="sub">Введите логин и пароль администратора</div>
    <form id="loginForm">
      <label for="lu">Логин</label>
      <input id="lu" autocomplete="username" required>
      <label for="lp">Пароль</label>
      <input id="lp" type="password" autocomplete="current-password" required>
      <button type="submit">Войти ↗</button>
    </form>
    <div id="loginErr" class="login-err" style="display:none"></div>
  </div>
</div>

<div id="app" style="display:none">
  <div class="topbar">
    <div class="logo">dreamTunnel — панель</div>
    <div class="right">
      <span class="dim" id="clock"></span>
      <a id="logout">Выйти ↗</a>
    </div>
  </div>

  <div class="wrap">
    <div class="page-heading">
      <div class="idx">01 / 04 · обзор</div>
      <h2 id="h2title">Состояние сервера</h2>
      <div class="lead" id="leadtxt">Активные туннели, трафик и посетители — в реальном времени.</div>
    </div>

    <div class="stats">
      <div class="stat"><div class="v" id="stActive">0</div><div class="l">активных туннелей</div></div>
      <div class="stat"><div class="v" id="stStreams">0</div><div class="l">соединений всего</div></div>
      <div class="stat"><div class="v" id="stIn">0</div><div class="l">трафик к клиентам</div></div>
      <div class="stat"><div class="v" id="stOut">0</div><div class="l">трафик от клиентов</div></div>
    </div>

    <div class="strip" id="msg"></div>

    <div class="sec">
      <h3>Активные туннели <span class="cnt" id="cntActive"></span></h3>
      <div class="table-wrap"><table>
        <thead><tr>
          <th>Домен</th><th>Режим</th><th>Лок. порт</th><th>IP клиента</th>
          <th>Начало</th><th>Активен</th><th>Соединений</th><th>Трафик</th><th></th>
        </tr></thead>
        <tbody id="tbActive"></tbody>
      </table></div>
    </div>

    <div class="sec">
      <h3>История сессий <span class="cnt" id="cntHist"></span></h3>
      <div class="table-wrap"><table>
        <thead><tr>
          <th>Домен</th><th>Режим</th><th>IP</th><th>Начало</th><th>Конец</th>
          <th>Соединений</th><th>Трафик</th><th>Статус</th>
        </tr></thead>
        <tbody id="tbHist"></tbody>
      </table></div>
    </div>

    <div class="sec">
      <h3>Блокировки IP</h3>
      <div class="ban-form">
        <input id="banIP" placeholder="IP-адрес">
        <button id="banBtn">Заблокировать</button>
      </div>
      <div class="table-wrap"><table>
        <thead><tr><th>IP</th><th>До</th><th></th></tr></thead>
        <tbody id="tbBans"></tbody>
      </table></div>
    </div>

  </div>
</div>

<script>
let authed = false;

const $ = id => document.getElementById(id);
const esc = s => String(s ?? '').replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c]));
const fmtB = n => { n = Number(n)||0;
  if (n > 1024*1024*1024) return (n/1024/1024/1024).toFixed(1)+' GB';
  if (n > 1024*1024) return (n/1024/1024).toFixed(1)+' MB';
  if (n > 1024) return (n/1024).toFixed(1)+' KB';
  return n+' B'; };
const fmtT = t => { if(!t) return '—';
  const d = new Date(t);
  return d.toLocaleString('ru-RU', {day:'2-digit',month:'2-digit',hour:'2-digit',minute:'2-digit'}); };
const fmtDur = s => { s = Math.max(0, Math.floor((Date.now()-new Date(s))/1000));
  if (s < 60) return s+' с';
  if (s < 3600) return Math.floor(s/60)+' мин';
  return Math.floor(s/3600)+' ч'; };
const short = s => s && s.length > 14 ? s.slice(0,8)+'…' : s;

function msg(text, ok) {
  const el = $('msg');
  el.textContent = text;
  el.className = 'strip ' + (ok ? 'ok' : 'err');
  setTimeout(() => { el.className='strip'; }, 4000);
}

async function api(path, body) {
  const r = await fetch('/api/'+path, {
    method: body ? 'POST' : 'GET',
    headers: body ? {'Content-Type':'application/json'} : undefined,
    body: body ? JSON.stringify(body) : undefined,
    credentials: 'same-origin',
  });
  if (r.status === 401 || (path !== 'login' && (await r.clone().json().catch(()=>({}))).error === 'unauthorized')) {
    authed = false; showLogin(); throw new Error('unauthorized');
  }
  return r.json();
}

function showLogin() { $('app').style.display='none'; $('login').style.display='grid'; }
function showApp()  { $('login').style.display='none'; $('app').style.display='block'; }

async function boot() {
  try {
    const st = await api('state');
    authed = true; showApp(); render(st);
  } catch (e) { showLogin(); }
}

$('loginForm').addEventListener('submit', async e => {
  e.preventDefault();
  $('loginErr').style.display = 'none';
  try {
    const r = await api('login', {user: $('lu').value, pass: $('lp').value});
    if (r.error) { $('loginErr').textContent = r.error; $('loginErr').style.display = 'block'; return; }
    authed = true; $('lp').value=''; showApp(); await refresh();
  } catch (err) {}
});

$('logout').addEventListener('click', async () => {
  try { await api('logout', {}); } catch (e) {}
  authed = false; showLogin();
});

$('banBtn').addEventListener('click', async () => {
  const ip = $('banIP').value.trim();
  if (!ip) return;
  try {
    const r = await api('ban', {ip});
    if (r.ok) { msg('IP '+ip+' заблокирован', true); $('banIP').value=''; refresh(); }
    else msg(r.error || 'Ошибка', false);
  } catch (e) {}
});

function render(st) {
  $('stActive').textContent = st.clients.length;
  $('stStreams').textContent = st.stats.totalStreams;
  $('stIn').textContent = fmtB(st.stats.totalBytesIn);
  $('stOut').textContent = fmtB(st.stats.totalBytesOut);
  $('cntActive').textContent = '('+st.clients.length+')';
  $('cntHist').textContent = '('+st.history.length+')';

  // active tunnels
  $('tbActive').innerHTML = st.clients.map(c => '<tr>' +
    '<td class="mono">' + esc(c.Host) + '</td>' +
    '<td>' + esc(c.Mode) + '</td>' +
    '<td class="mono">' + esc(c.Port) + '</td>' +
    '<td class="mono">' + esc(c.IP) + '</td>' +
    '<td>' + fmtT(c.Started) + '</td>' +
    '<td>' + fmtDur(c.LastSeen) + ' назад</td>' +
    '<td>' + (c.StreamsNow||0) + ' / ' + (c.Streams||0) + '</td>' +
    '<td class="dim">↓' + fmtB(c.BytesIn) + ' ↑' + fmtB(c.BytesOut) + '</td>' +
    '<td><button class="act danger" onclick="killSess(\''+esc(c.Sess)+'\',\''+esc(c.Host)+'\')">Разорвать</button> ' +
    '<button class="act danger" onclick="banIPof(\''+esc(c.IP)+'\')">Бан IP</button></td>' +
    '</tr>').join('') || '<tr><td colspan="9" class="dim" style="padding:40px 0">Нет активных туннелей</td></tr>';

  // history
  $('tbHist').innerHTML = st.history.slice(0, 100).map(h => '<tr>' +
    '<td class="mono">' + esc(h.host) + '</td>' +
    '<td>' + esc(h.mode) + '</td>' +
    '<td class="mono">' + esc(h.ip) + '</td>' +
    '<td>' + fmtT(h.start) + '</td>' +
    '<td>' + fmtT(h.end) + '</td>' +
    '<td>' + (h.streams||0) + '</td>' +
    '<td class="dim">↓' + fmtB(h.bytesIn) + ' ↑' + fmtB(h.bytesOut) + '</td>' +
    '<td>' + (h.active ? '<span class="badge on">активна</span>'
        : h.killed ? '<span class="badge killed">разорвана</span>'
        : '<span class="badge off">закрыта</span>') + '</td>' +
    '</tr>').join('') || '<tr><td colspan="8" class="dim" style="padding:40px 0">История пуста</td></tr>';

  // bans
  const bans = Object.entries(st.bans || {});
  $('tbBans').innerHTML = bans.map(([ip, until]) => '<tr>' +
    '<td class="mono">' + esc(ip) + '</td>' +
    '<td>' + esc(until) + '</td>' +
    '<td><button class="act" onclick="unbanIP(\''+esc(ip)+'\')">Разблокировать</button></td>' +
    '</tr>').join('') || '<tr><td colspan="3" class="dim" style="padding:40px 0">Нет блокировок</td></tr>';
}

async function refresh() {
  if (!authed) return;
  try { render(await api('state')); } catch (e) {}
}

window.killSess = async (sess, host) => {
  try {
    const r = await api('kill', {sess, host});
    if (r.ok) { msg('Сессия '+host+' разорвана', true); refresh(); }
    else msg(r.error || 'Не найдено', false);
  } catch (e) {}
};
window.banIPof = async ip => {
  if (!confirmIP(ip)) return;
  try {
    const r = await api('ban', {ip});
    if (r.ok) { msg('IP '+ip+' заблокирован, соединения разорваны', true); refresh(); }
  } catch (e) {}
};
window.unbanIP = async ip => {
  try {
    const r = await api('unban', {ip});
    if (r.ok) { msg('IP '+ip+' разблокирован', true); refresh(); }
  } catch (e) {}
};
function confirmIP(ip) { return window.confirm('Заблокировать '+ip+' и разорвать его сессии?'); }

setInterval(() => { if (authed) refresh(); }, 5000);
setInterval(() => {
  const d = new Date();
  $('clock').textContent = d.toLocaleTimeString('ru-RU');
}, 1000);

boot();
</script>
</body>
</html>`
