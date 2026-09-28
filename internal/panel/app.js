'use strict';
/* ── 状态 ─────────────────────────────────────────────────────────── */
const LS_KEY = 'wb2api.key', LS_THEME = 'wb2api.theme';
let theme = localStorage.getItem(LS_THEME) || 'auto';   // auto | light | dark
let view = 'accounts';
let overviewData = null, cfgLoaded = null;
let logPin = true, loginState = null, loginTimer = null;
let refTimer = null;

const $ = id => document.getElementById(id);

/* ── 主题 ─────────────────────────────────────────────────────────── */
/* 两态翻转（浅/深），首次访问跟随系统偏好；点击总是切换可见外观，符合直觉。 */
function effTheme() {
  return theme === 'auto' ? (matchMedia('(prefers-color-scheme: light)').matches ? 'light' : 'dark') : theme;
}
function applyTheme() {
  const eff = effTheme();
  document.documentElement.dataset.theme = eff;
  $('icoTheme').innerHTML = eff === 'light'
    ? '<circle cx="8" cy="8" r="3"/><path d="M8 1v2M8 13v2M1 8h2M13 8h2M3.2 3.2l1.4 1.4M11.4 11.4l1.4 1.4M12.8 3.2l-1.4 1.4M4.6 11.4l-1.4 1.4"/>'
    : '<path d="M13.2 9.6A5.6 5.6 0 0 1 6.4 2.8a5.6 5.6 0 1 0 6.8 6.8z"/>';
  $('btnTheme').title = eff === 'light' ? '切换到深色' : '切换到浅色';
}
addEventListener('change', applyTheme);
$('btnTheme').onclick = () => {
  theme = effTheme() === 'light' ? 'dark' : 'light';
  localStorage.setItem(LS_THEME, theme);
  applyTheme();
};
applyTheme();

/* ── 请求 ─────────────────────────────────────────────────────────── */
async function api(path, opts = {}) {
  const h = Object.assign({}, opts.headers || {});
  const k = localStorage.getItem(LS_KEY);
  if (k) h['Authorization'] = 'Bearer ' + k;
  if (opts.body) h['Content-Type'] = 'application/json';
  const r = await fetch('/panel/api/' + path, Object.assign({}, opts, { headers: h }));
  if (r.status === 401) { openKey(); throw new Error('密钥无效或未填写'); }
  const d = await r.json().catch(() => ({}));
  if (!r.ok) throw new Error(d.error || ('HTTP ' + r.status));
  return d;
}
function toast(msg, cls) {
  const el = document.createElement('div');
  el.className = 'tst ' + (cls || '');
  el.textContent = msg;
  $('toasts').appendChild(el);
  setTimeout(() => el.remove(), 3600);
}
// esc 文本/属性双安全转义。不能只用 div.innerHTML（它转义 <>& 但不转义引号），
// 否则字符串拼进 HTML 属性（如 title="uid: ..."）时引号可闭合属性并注入事件处理器。
// 显式替换 5 个字符：& < > " '（& 必须最先，避免二次转义）。
function esc(s) {
  return String(s == null ? '' : s)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;')
    .replace(/'/g, '&#39;');
}
function ago(iso) {
  if (!iso || iso.startsWith('0001-')) return '—';
  const s = (Date.now() - new Date(iso)) / 1000;
  if (s < 0) return '刚刚';
  if (s < 60) return Math.floor(s) + ' 秒前';
  if (s < 3600) return Math.floor(s / 60) + ' 分钟前';
  if (s < 86400) return Math.floor(s / 3600) + ' 小时前';
  return Math.floor(s / 86400) + ' 天前';
}
function dur(sec) {
  sec = Math.max(0, Math.round(sec));
  const h = Math.floor(sec / 3600), m = Math.floor(sec % 3600 / 60), s = sec % 60;
  return h ? h + '时' + String(m).padStart(2, '0') + '分' : m ? m + '分' + String(s).padStart(2, '0') + '秒' : s + '秒';
}

function formatTokenCount(tokens) {
  if (tokens == null || tokens === '') return '—';
  const n = Number(tokens);
  if (!Number.isFinite(n) || n < 0) return '—';
  if (n < 1000) return String(Math.round(n));
  const units = [['k', 1e3], ['m', 1e6], ['b', 1e9]];
  let unit = units[0];
  for (const candidate of units) {
    if (n >= candidate[1]) unit = candidate;
  }
  let value = n / unit[1];
  let rounded = Number(value.toFixed(1));
  // 999999 → 1m，而不是 1000k；四舍五入后自动升级单位。
  const next = units[units.indexOf(unit) + 1];
  if (next && rounded >= 1000) {
    unit = next;
    value = n / unit[1];
    rounded = Number(value.toFixed(1));
  }
  return rounded + unit[0];
}

function formatLatency(ms) {
  if (ms == null || ms === '') return '—';
  const n = Number(ms);
  if (!Number.isFinite(n) || n <= 0) return '—';
  return n < 1000 ? Math.round(n) + 'ms' : (n / 1000).toFixed(1).replace(/\.0$/, '') + 's';
}
function formatRate(rate) {
  if (rate == null || rate === '') return '—';
  const n = Number(rate);
  if (!Number.isFinite(n) || n < 0) return '—';
  return n.toFixed(1) + 'tok/s';
}

/* ── 密钥门 ───────────────────────────────────────────────────────── */
function openKey() { $('keyVeil').classList.add('on'); setTimeout(() => $('keyInput').focus(), 60); }
$('btnKey').onclick = async () => {
  const v = $('keyInput').value.trim();
  if (!v) return;
  localStorage.setItem(LS_KEY, v);
  try {
    await api('overview');
    $('keyErr').hidden = true;
    $('keyVeil').classList.remove('on');
    start();
  } catch (e) { $('keyErr').hidden = false; }
};
$('keyInput').addEventListener('keydown', e => { if (e.key === 'Enter') $('btnKey').click(); });

/* ── 路由 ─────────────────────────────────────────────────────────── */
const TITLES = { accounts: '账号池', usage: '用量', packages: '积分构成', taskscenter: '任务中心', models: '模型路由', tokens: 'API 令牌', config: '配置', logs: '运行日志' };
function go(v) {
  view = v;
  document.querySelectorAll('.view').forEach(s => s.hidden = s.id !== 'view-' + v);
  document.querySelectorAll('.nav a').forEach(a => a.classList.toggle('on', a.dataset.view === v));
  $('ttl').textContent = TITLES[v] || v;
  if (v === 'models') { loadModelRoutes(); if (!$('mdBody').children.length) loadModels(); }
  if (v === 'tokens') loadTokens();
  if (v === 'config') loadConfig();
  if (v === 'logs') loadLogs();
  if (v === 'usage') loadUsage();
  if (v === 'packages') loadPackages();
  if (v === 'taskscenter') reattachQueueView();
}
document.querySelectorAll('.nav a').forEach(a => a.onclick = e => { e.preventDefault(); go(a.dataset.view); history.replaceState(null, '', '#' + a.dataset.view); });
go((location.hash || '#accounts').slice(1) in TITLES ? (location.hash || '#accounts').slice(1) : 'accounts');

/* ── 账号池 ───────────────────────────────────────────────────────── */
function renderAccounts(list) {
  const tb = $('accBody');
  if (!list.length) {
    tb.innerHTML = '<tr><td colspan="9"><div class="empty"><div class="big">账号池是空的</div>点击右上角「添加账号」，用浏览器登录一个 WorkBuddy 账号</div></td></tr>';
    return;
  }
  // 有总额度（credits_total）→ 进度条按自身 剩余/总额 百分比；旧数据无总额 → 退回池内最高=100%
  const maxCred = Math.max(1, ...list.map(s => s.credits || 0));
  tb.innerHTML = list.map(s => {
    const bl = (new Date(s.breaker_until || 0) - Date.now()) / 1000;
    const dg = (new Date(s.degrade_until || 0) - Date.now()) / 1000;
    const cool = Math.max(s.cool_remaining_sec || 0, bl > 0 ? bl : 0, dg > 0 ? dg : 0);
    let cls = '', tag;
    if (s.disabled) { cls = 'off'; tag = '<span class="tag bad">已禁用</span>'; }
    else if (cool > 0) {
      cls = 'cool';
      const kind = bl > Math.max(s.cool_remaining_sec || 0, dg > 0 ? dg : 0) ? '熔断'
        : (dg > (s.cool_remaining_sec || 0) ? '连败降权' : (s.cool_kind === 'hard_credit' ? '积分冷却' : '限流冷却'));
      tag = '<span class="tag warn">' + kind + ' · ' + dur(cool) + '</span>';
    } else tag = '<span class="tag ok">可用</span>' + (s.in_flight ? '' : '');
    const note = s.reason ? '<div class="hint" style="font-size:11.5px;color:var(--ink-3);margin-top:3px">' + esc(s.reason) + '</div>' : '';
    const short = s.uid.length > 16 ? s.uid.slice(0, 16) + '…' : s.uid;
    const cred = s.credits == null ? '—' : (s.credits_total > 0 ? s.credits + '<span class="of">/' + s.credits_total + '</span>' : String(s.credits));
    const pct = s.credits_total > 0
      ? Math.min(100, Math.round((s.credits || 0) / s.credits_total * 100))
      : Math.round((s.credits || 0) / maxCred * 100);
    // 成本台账 tooltip（model_costs）：每模型实测单价（≤0 = 实测免费），运维据此
    // 看「为什么总选它」——免费号垄断 / 单价排序一眼可见。
    let credTip = s.credits_total > 0 ? '剩余 ' + s.credits + ' / 总额 ' + s.credits_total + '（' + pct + '%）' : '积分（相对池内最高）';
    const costs = (s.model_costs || []).filter(c => c.model);
    if (costs.length) {
      credTip += '\n实测单价（credits/1K）：\n' + costs.map(c =>
        '  ' + c.model + '：' + (c.cost_per_1k <= 0 ? '免费' : c.cost_per_1k)).join('\n');
    }
    const frozen = s.disabled || cool > 0;
    const tu = s.token_usage || {};
    const req = tu.request_count || 0;
    const totalTok = formatTokenCount(tu.total_tokens);
    const totalTokUnit = totalTok === '—' ? '' : '<em>tok</em>';
    const latency = formatLatency(tu.last_latency_ms);
    const rate = formatRate(tu.last_tokens_per_second);
    const usageTitle = '最近一次：' + req + ' 次 / ' + totalTok + ' / 延迟 ' + latency + ' / ' + rate;
    return '<tr class="' + cls + '" title="uid: ' + esc(s.uid) + '">' +
      '<td class="mark" aria-hidden="true"><i></i></td>' +
      '<td class="who"><div class="nm">' + (s.nickname ? esc(s.nickname) : '<span style="color:var(--ink-3)">未命名</span>') + (s.realm === 'global' ? ' <span class="realm-tag">国际版</span>' : '') + '</div><div class="id">' + esc(short) + '</div></td>' +
      '<td>' + tag + note + '</td>' +
      '<td class="cred" title="' + esc(credTip) + '"><div class="n">' + cred + '</div><div class="bar"><i style="width:' + pct + '%"></i></div></td>' +
      '<td class="num">' + (s.success_count || 0) + ' <span style="color:var(--ink-3)">/</span> <span style="color:var(--bad)">' + (s.err_total || 0) + '</span></td>' +
      '<td class="num">' + (s.in_flight || 0) + '</td>' +
      '<td class="num usage-cell" title="' + esc(usageTitle) + '"><span class="usage-line" aria-label="' + esc(usageTitle) + '">' +
        '<span class="usage-item usage-count"><b>' + req + '</b><em>次</em></span>' +
        '<span class="usage-item usage-total"><b>' + totalTok + '</b>' + totalTokUnit + '</span>' +
        '<span class="usage-item usage-latency"><b>' + latency + '</b></span>' +
        '<span class="usage-item usage-rate"><b>' + rate + '</b></span>' +
      '</span></td>' +
      '<td class="num" style="color:var(--ink-3)">' + ago(s.last_success) + '</td>' +
      '<td class="acts">' +
        '<button class="xs ghost" data-a="checkin" data-u="' + esc(s.uid) + '">签到</button>' +
        '<button class="xs ghost" data-a="balance" data-u="' + esc(s.uid) + '">余额</button>' +
        '<button class="xs ghost" data-a="tasks" data-u="' + esc(s.uid) + '">任务</button>' +
        (frozen ? '<button class="xs primary" data-a="revive" data-u="' + esc(s.uid) + '">解冻</button>'
                : '<button class="xs ghost" data-a="disable" data-u="' + esc(s.uid) + '">禁用</button>') +
        '<button class="xs ghost danger" data-a="remove" data-u="' + esc(s.uid) + '">移除</button>' +
      '</td></tr>';
  }).join('');
}

async function loadOverview(quiet) {
  try {
    const d = await api('overview');
    overviewData = d;
    $('sTotal').textContent = d.total;
    $('sHealthy').textContent = d.healthy;
    $('sCooling').textContent = d.cooling;
    $('sDisabled').textContent = d.disabled;
    const remSum = (d.accounts || []).reduce((a, s) => a + (s.credits || 0), 0);
  const totSum = (d.accounts || []).reduce((a, s) => a + (s.credits_total || 0), 0);
  $('sCredits').textContent = totSum > 0 ? remSum + ' / ' + totSum : remSum;
    $('sSticky').textContent = d.sticky_sessions;
    $('navSub').textContent = 'v' + d.version;
    $('navVer').textContent = 'v' + d.version;
    $('navRedis').textContent = d.redis_mode === 'upstash' ? 'Redis 镜像' : '本地内存';
    $('navState').textContent = d.healthy > 0 ? '服务正常' : (d.total ? '无可用账号' : '待添加账号');
    const p = $('navPulse');
    p.className = 'pulse' + (d.healthy > 0 ? '' : (d.total ? ' warn' : ' bad'));
    $('accNote').textContent = d.in_flight_full ? d.in_flight_full + ' 个账号在途占满' : '';
    const up = Math.floor(d.uptime_sec);
    $('subMeta').textContent = '运行 ' + (up >= 86400 ? Math.floor(up / 86400) + ' 天 ' : '') + Math.floor(up % 86400 / 3600) + ' 时 ' + Math.floor(up % 3600 / 60) + ' 分';
    renderAccounts(d.accounts || []);
  } catch (e) { if (!quiet) toast(e.message, 'err'); }
}

$('accBody').addEventListener('click', async ev => {
  const b = ev.target.closest('button[data-a]');
  if (!b) return;
  const u = b.dataset.u, a = b.dataset.a;
  if (a === 'remove' && !confirm('移除账号将删除池状态与 auths/ 下的凭证文件，且不可恢复。确认移除？')) return;
  if (a === 'disable' && !confirm('禁用后该账号不再参与选号，需手动解冻才能恢复。确认禁用？')) return;
  b.disabled = true;
  try {
    if (a === 'checkin') {
      const r = await api('accounts/' + encodeURIComponent(u) + '/checkin', { method: 'POST' });
      toast('签到完成' + (r.credits != null ? '，积分 ' + r.credits + (r.credits_total > 0 ? '/' + r.credits_total : '') : '') + (r.checkin_message ? '（' + r.checkin_message + '）' : ''), 'ok');
    } else if (a === 'balance') {
      const r = await api('accounts/' + encodeURIComponent(u) + '/balance', { method: 'POST' });
      toast('余额已更新：' + r.credits + (r.credits_total > 0 ? ' / ' + r.credits_total : ''), 'ok');
    } else if (a === 'revive') {
      await api('accounts/' + encodeURIComponent(u) + '/revive', { method: 'POST' });
      toast('已解冻', 'ok');
    } else if (a === 'disable') {
      await api('accounts/' + encodeURIComponent(u) + '/disable', { method: 'POST' });
      toast('已禁用', 'ok');
    } else if (a === 'tasks') {
      openTasks(u);
    } else if (a === 'remove') {
      const r = await api('accounts/' + encodeURIComponent(u) + '/remove', { method: 'POST' });
      toast(r.file_error ? '已移除（凭证文件删除失败：' + r.file_error + '）' : '已移除', 'ok');
    }
  } catch (e) { toast(e.message, 'err'); }
  finally { b.disabled = false; loadOverview(true); }
});

$('btnCheckinAll').onclick = async () => {
  try { await api('checkin_all', { method: 'POST' }); toast('全部签到已开始，结果见日志', 'ok'); }
  catch (e) { toast(e.message, 'err'); }
};
$('btnKeepaliveAll').onclick = async () => {
  try { await api('keepalive_all', { method: 'POST' }); toast('全部保活已开始，结果见日志', 'ok'); }
  catch (e) { toast(e.message, 'err'); }
};
$('btnTravelAll').onclick = async () => {
  try { await api('travel_all', { method: 'POST' }); toast('旅行巡检已开始（含领养链路），结果见日志', 'ok'); }
  catch (e) { toast(e.message, 'err'); }
};
$('btnActivityAll').onclick = async () => {
  try { await api('activity_all', { method: 'POST' }); toast('活跃上报已开始，结果见日志', 'ok'); }
  catch (e) { toast(e.message, 'err'); }
};

/* ── 模型 ─────────────────────────────────────────────────────────── */
/* 实测上限标注：scripts/probe_max_tokens.py --panel-out 写入探测结果，
   /panel/api/model_probes 只读透传。探测键带域前缀（cn:glm-5.2），模型表
   显示裸名，按「精确命中或 :后缀」关联。无数据时本列退回上游声称值。 */
function fmtK(n) { n = Number(n || 0); return n >= 1000 ? Math.round(n / 1000) + 'K' : String(n); }
function probeDays(ts) {
  if (!ts) return null;
  const t = new Date(String(ts).replace(' ', 'T'));
  const d = (Date.now() - t.getTime()) / 86400000;
  return isNaN(d) ? null : Math.floor(d);
}
function outCell(m, pr) {
  if (!pr) return '<td class="num">' + (m.max_output_tokens ? fmtK(m.max_output_tokens) : '—') + '</td>';
  const tip = '声称 ' + (pr.claimed ? fmtK(pr.claimed) : '?') + ' · 实测 ' + (pr.measured ? fmtK(pr.measured) : '?') +
    (pr.note ? ' · ' + pr.note : '') + (pr.tested_at ? ' · 探测于 ' + pr.tested_at : '');
  const days = probeDays(pr.tested_at);
  const stale = days !== null && days > 30 ? ' · ' + days + ' 天前' : '';
  if (pr.verdict === 'clamped' && pr.measured) {
    if (pr.claimed && pr.measured < pr.claimed) {
      const x = pr.claimed / pr.measured;
      const xs = (x >= 10 ? Math.round(x) : Math.round(x * 10) / 10) + '×';
      return '<td class="num" title="' + esc(tip) + '"><span style="color:var(--warn);font-weight:600">' +
        fmtK(pr.measured) + ' ⚠</span><div class="note">钳制 ' + xs + stale + '</div></td>';
    }
    return '<td class="num" title="' + esc(tip) + '"><span style="color:var(--ok)">' + fmtK(pr.measured) +
      (pr.claimed && pr.measured > pr.claimed ? ' ↑' : ' ✓') + '</span></td>';
  }
  if (pr.verdict === 'at_least' && pr.measured)
    return '<td class="num" title="' + esc(tip) + '"><span style="color:var(--ink-3)">≥' + fmtK(pr.measured) + '</span></td>';
  return '<td class="num" title="' + esc(tip) + '"><span style="color:var(--ink-3)">?</span><div class="note">未测出' + stale + '</div></td>';
}

/* rateCell 倍率列：牌价 vs 生效价。上游 credits 是牌价（转正后基准倍率），
   modelPromotions 给当前生效折扣（限时免费 factor=0 / 夜间五折 0.5 等）——
   WorkBuddy 客户端显示的正是生效价。有折扣：生效价大字 + 标签 + 划线牌价，
   悬停带时段说明；无 factor 只有标签（错峰类）：牌价 + 标签。 */
function rateCell(m) {
  const tip = m.promo_note ? ' title="' + esc(m.promo_note) + '"' : '';
  if (m.promo_factor != null && m.promo_credits) {
    const base = m.credits ? ' <s style="color:var(--ink-3);font-size:11.5px">' + esc(m.credits) + '</s>' : '';
    const label = m.promo_label ? ' <span class="tag ok">' + esc(m.promo_label) + '</span>' : '';
    return '<span' + tip + ' style="cursor:help"><b>' + esc(m.promo_credits) + '</b>' + label + base + '</span>';
  }
  if (m.promo_label) {
    return '<span' + tip + ' style="cursor:help">' + (m.credits ? esc(m.credits) : '—') +
      ' <span class="tag warn">' + esc(m.promo_label) + '</span></span>';
  }
  return m.credits ? esc(m.credits) : '—';
}

/* ── 模型路由 (中转) ───────────────────────────────────────────────── */
let routesLoaded = null;
let tokensLoaded = null;
let circuitCache = []; // 模型级熔断降级状态缓存

async function fetchCircuitStatus() {
  try {
    const data = await api('circuit-status');
    circuitCache = Array.isArray(data) ? data : [];
  } catch (_) {
    circuitCache = [];
  }
}

function circuitBadge(modelId, target) {
  // 按 route.id 或 route.target 查找降级状态
  const item = circuitCache.find(c => c.model === modelId || c.model === target);
  if (!item) return '';
  if (item.status === 'degraded') {
    const sec = item.remaining_sec || 0;
    const timeStr = sec >= 60 ? Math.ceil(sec / 60) + '分钟' : sec + '秒';
    return ' <span class="tag warn" style="font-size:11px;" title="全池429/额度耗尽，自动降级至备选模型">🔴 降级中 → ' +
      esc(item.active_fallback || '?') + ' (' + timeStr + '后恢复)</span>' +
      ' <button type="button" class="xs ghost" data-act="circuit-revive" data-model="' + esc(item.model) + '" title="手动恢复主模型">⚡恢复</button>';
  }
  if (item.status === 'probing') {
    return ' <span class="tag" style="font-size:11px;background:var(--warn-bg);color:var(--warn);" title="恢复窗口已到，等待下次请求验证主模型">🟡 探测中</span>';
  }
  return '';
}

function renderModelRoutes() {
  const tb = $('routesBody');
  if (!tb) return;
  if (!routesLoaded || !routesLoaded.length) {
    tb.innerHTML = '<tr><td colspan="8"><div class="empty">暂无中转路由规则，点击右上角「添加路由」自定义模型转发</div></td></tr>';
    return;
  }
  tb.innerHTML = routesLoaded.map((r, i) => {
    const enabledTag = r.enabled
      ? '<button type="button" class="xs chip on" data-act="toggle-route" data-idx="' + i + '">已启用</button>'
      : '<button type="button" class="xs chip" data-act="toggle-route" data-idx="' + i + '">已禁用</button>';
    const fb = (r.fallbacks && r.fallbacks.length)
      ? r.fallbacks.map(f => '<span class="tag ok">' + esc(f) + '</span>').join(' ')
      : '<span style="color:var(--ink-3)">—</span>';
    const limit = [];
    if (r.max_in_flight > 0) limit.push(r.max_in_flight + ' 在途');
    if (r.rpm > 0) limit.push(r.rpm + ' RPM');
    const limitHtml = limit.length ? limit.join(' / ') : '<span style="color:var(--ink-3)">不限</span>';
    const reasonTag = r.reasoning
      ? '<span class="tag ok">强制开启' + (r.effort ? ' · ' + esc(r.effort) : '') + '</span>'
      : '<span style="color:var(--ink-3)">默认</span>';
    const badge = circuitBadge(r.id, r.target);
    return '<tr>' +
      '<td>' + enabledTag + '</td>' +
      '<td><b>' + esc(r.id) + '</b>' + badge + '</td>' +
      '<td><span class="tag">' + esc(r.target) + '</span></td>' +
      '<td>' + fb + '</td>' +
      '<td class="num">' + limitHtml + '</td>' +
      '<td>' + reasonTag + '</td>' +
      '<td style="color:var(--ink-2);font-size:12px;">' + esc(r.description || '—') + '</td>' +
      '<td class="acts">' +
        '<button type="button" class="xs ghost" data-act="test-route" data-idx="' + i + '">测试</button>' +
        '<button type="button" class="xs ghost" data-act="edit-route" data-idx="' + i + '">编辑</button>' +
        '<button type="button" class="xs ghost danger" data-act="del-route" data-idx="' + i + '">删除</button>' +
      '</td>' +
    '</tr>';
  }).join('');
}

async function loadModelRoutes() {
  try {
    const [d] = await Promise.all([api('config'), fetchCircuitStatus()]);
    cfgLoaded = d.config;
    routesLoaded = (cfgLoaded && cfgLoaded.model_routes) ? cfgLoaded.model_routes : [];
    renderModelRoutes();
  } catch (e) {
    const tb = $('routesBody');
    if (tb) tb.innerHTML = '<tr><td colspan="8"><div class="empty">加载路由失败：' + esc(e.message) + '</div></td></tr>';
  }
}

async function saveRoutesConfig() {
  try {
    if (cfgLoaded) {
      cfgLoaded.model_routes = routesLoaded;
    }
    const res = await api('config', { method: 'POST', body: JSON.stringify({ model_routes: routesLoaded }) });
    if (!res || res.ok === false) {
      throw new Error(res && res.error ? res.error : '保存失败');
    }
    toast('模型路由配置已保存并立即生效', 'ok');
    const d = await api('config');
    cfgLoaded = d.config;
    routesLoaded = (cfgLoaded && cfgLoaded.model_routes) ? cfgLoaded.model_routes : [];
    renderModelRoutes();
  } catch (e) {
    toast('保存路由失败: ' + e.message, 'err');
    try {
      const d = await api('config');
      cfgLoaded = d.config;
      routesLoaded = (cfgLoaded && cfgLoaded.model_routes) ? cfgLoaded.model_routes : [];
      renderModelRoutes();
    } catch (_) {}
  }
}

/* ── 模型厂商智能分组引擎 ───────────────────────────────────────────── */
const VENDOR_GROUPS = [
  {
    name: '🌟 官方智能调度',
    match: (id, name) => /^(auto|default-model|fast-model|balanced-model|primary-model|deep-model)($|-)/i.test(id) || /^(auto|default|fast|balanced|primary|deep)-model/i.test(name)
  },
  {
    name: '🤖 DeepSeek (深度求索)',
    match: (id, name) => /deepseek/i.test(id) || /deepseek/i.test(name)
  },
  {
    name: '🔮 Kimi (月之暗面)',
    match: (id, name) => /kimi/i.test(id) || /kimi/i.test(name)
  },
  {
    name: '⚡ 智谱 GLM',
    match: (id, name) => /glm/i.test(id) || /glm/i.test(name) || /智谱/i.test(name)
  },
  {
    name: '🐉 腾讯混元 (Hunyuan)',
    match: (id, name) => /(^|-)(hy\d|hunyuan)/i.test(id) || /^hy\d/i.test(id) || /hunyuan|混元/i.test(name) || /^hy\d/i.test(name)
  },
  {
    name: '🌀 MiniMax (稀宇科技)',
    match: (id, name) => /minimax/i.test(id) || /minimax/i.test(name)
  },
  {
    name: '🧠 OpenAI (GPT)',
    match: (id, name) => /(^|-)(gpt|o1|o3|openai)/i.test(id) || /openai|gpt/i.test(name)
  },
  {
    name: '🎭 Anthropic Claude',
    match: (id, name) => /claude/i.test(id) || /claude/i.test(name)
  },
  {
    name: '♊ Google Gemini',
    match: (id, name) => /gemini/i.test(id) || /gemini/i.test(name)
  },
  {
    name: '📦 其他模型',
    match: () => true
  }
];

function modelSortScore(m) {
  let score = 0;
  const id = (m._id || m.raw_id || m.id || '').toLowerCase();
  const name = (m.name || '').toLowerCase();
  const desc = (m.description || '').toLowerCase();
  const text = id + ' ' + name + ' ' + desc;
  if (/旗舰|flagship/.test(text)) score += 500;
  if (/\b(pro|max|ultra|plus)\b/.test(text)) score += 300;
  if (/preview/.test(text)) score += 100;
  if (/flash|lite|mini|极速|轻量/.test(text)) score -= 200;
  if (m.credits) {
    const num = parseFloat(String(m.credits).replace(/[^0-9.]/g, ''));
    if (!isNaN(num)) score += num * 100;
  }
  const verMatch = text.match(/v?(\d+(\.\d+)?)/);
  if (verMatch) {
    score += parseFloat(verMatch[1]) * 10;
  }
  return score;
}

function buildGroupedModelOptions(models, selectedValue, placeholder) {
  let html = '';
  if (placeholder) {
    html += `<option value="">${esc(placeholder)}</option>`;
  }
  const list = Array.isArray(models) ? models : [];
  const curVal = selectedValue != null ? String(selectedValue).trim() : '';
  let found = false;

  const items = list.map(m => {
    const rawId = m.raw_id || (m.id ? String(m.id).replace(/^(cn|global):/, '') : '');
    if (curVal && rawId === curVal) found = true;
    return Object.assign({}, m, { _id: rawId });
  });

  if (curVal && !found) {
    html += `<option value="${esc(curVal)}" selected>[已绑定] ${esc(curVal)} (上游暂未列出)</option>`;
  }

  const groups = VENDOR_GROUPS.map(g => ({ name: g.name, match: g.match, items: [] }));
  for (const item of items) {
    const id = item._id;
    const name = item.name || id;
    for (const g of groups) {
      if (g.match(id, name)) {
        g.items.push(item);
        break;
      }
    }
  }

  for (const g of groups) {
    if (!g.items.length) continue;
    g.items.sort((a, b) => modelSortScore(b) - modelSortScore(a));
    html += `<optgroup label="${esc(g.name)}">`;
    for (const m of g.items) {
      const id = m._id;
      const cr = m.credits ? ('[' + m.credits + '] ') : '';
      const name = m.name || id;
      const desc = m.description ? (' - ' + m.description.slice(0, 35)) : '';
      const isSel = (id === curVal) ? ' selected' : '';
      html += `<option value="${esc(id)}"${isSel}>${esc(cr)}${esc(name)} (${esc(id)})${esc(desc)}</option>`;
    }
    html += `</optgroup>`;
  }
  return html;
}

async function populateRouteTargetModels(curTarget) {
  const models = await fetchUpstreamModels(false);
  const sel = $('routeTargetSelect');
  const fbSel = $('routeFallbackSelect');
  const dl = $('targetModelList');
  if (sel) {
    sel.innerHTML = buildGroupedModelOptions(models, curTarget, '-- 选择腾讯官方真实模型 (动态拉取) --');
    sel.onchange = () => {
      if (sel.value) {
        $('routeTarget').value = sel.value;
        if (!$('routeId').value.trim()) {
          $('routeId').value = sel.value;
        }
      }
    };
    if (curTarget) sel.value = curTarget;
  }
  if (fbSel) {
    fbSel.innerHTML = buildGroupedModelOptions(models, '', '-- 从上游真实模型中自选备用降级模型 --');
  }
  if (dl) {
    dl.innerHTML = models.map(m => {
      const id = m.raw_id || m.id.replace(/^(cn|global):/, '');
      const cr = m.credits ? ('[' + m.credits + '] ') : '';
      const name = m.name || id;
      const desc = m.description ? (' - ' + m.description.slice(0, 30)) : '';
      return `<option value="${esc(id)}">${esc(cr)}${esc(name)}${esc(desc)}</option>`;
    }).join('');
  }
}

function renderRouteFallbackChips() {
  const box = $('routeFallbackChips');
  if (!box) return;
  const raw = ($('routeFallbacks').value || '').trim();
  const list = raw ? raw.split(/[,，\s]+/).map(s => s.trim()).filter(Boolean) : [];
  if (!list.length) {
    box.innerHTML = '<span style="font-size:12px;color:var(--ink-3);">当前未配置降级模型 (主模型全池不可用时将直接报错)</span>';
    return;
  }
  box.innerHTML = list.map((fb, idx) => {
    return `<span class="tag ok" style="display:inline-flex;align-items:center;gap:4px;font-size:12px;">` +
      `<span>#${idx + 1} 降级至 <b>${esc(fb)}</b></span>` +
      `<a href="javascript:void(0)" data-rm-fb="${idx}" style="color:var(--err);text-decoration:none;font-weight:bold;margin-left:4px;" title="移除此降级模型">×</a>` +
      `</span>`;
  }).join(' ');
}

async function openRouteModal(idx, prefilledTarget) {
  $('routeEditIndex').value = String(idx);
  let curTarget = prefilledTarget || '';
  if (idx >= 0 && routesLoaded && routesLoaded[idx]) {
    const r = routesLoaded[idx];
    $('routeModalTitle').textContent = '编辑模型路由';
    $('routeId').value = r.id || '';
    curTarget = r.target || '';
    $('routeTarget').value = curTarget;
    $('routeFallbacks').value = (r.fallbacks || []).join(', ');
    $('routeMaxInFlight').value = r.max_in_flight || '';
    $('routeRPM').value = r.rpm || '';
    $('routeReasoning').value = r.reasoning ? 'force_on' : 'auto';
    $('routeEffort').value = r.effort || '';
    $('routeDesc').value = r.description || '';
    $('routeEnabled').checked = r.enabled !== false;
  } else {
    $('routeModalTitle').textContent = '添加模型路由';
    $('routeId').value = curTarget || '';
    $('routeTarget').value = curTarget;
    $('routeFallbacks').value = '';
    $('routeMaxInFlight').value = '';
    $('routeRPM').value = '';
    $('routeReasoning').value = 'auto';
    $('routeEffort').value = '';
    $('routeDesc').value = '';
    $('routeEnabled').checked = true;
  }
  renderRouteFallbackChips();
  $('routeVeil').classList.add('on');
  await populateRouteTargetModels(curTarget);
}

async function saveRoute() {
  const id = $('routeId').value.trim();
  const target = $('routeTarget').value.trim();
  if (!id) { $('routeId').focus(); toast('请输入外部模型 ID', 'err'); return; }
  if (!target) { $('routeTarget').focus(); toast('请输入上游目标模型', 'err'); return; }

  const fallbacks = $('routeFallbacks').value.split(/[,，\s]+/).map(s => s.trim()).filter(Boolean);
  const maxInFlight = parseInt($('routeMaxInFlight').value, 10) || 0;
  const rpm = parseInt($('routeRPM').value, 10) || 0;
  const reasonVal = $('routeReasoning').value;
  const reasoning = reasonVal === 'force_on';
  const effort = $('routeEffort').value;
  const description = $('routeDesc').value.trim();
  const enabled = $('routeEnabled').checked;

  const item = {
    id,
    target,
    fallbacks,
    max_in_flight: maxInFlight,
    rpm,
    reasoning,
    effort,
    description,
    enabled
  };

  const idx = parseInt($('routeEditIndex').value, 10);
  if (idx >= 0 && idx < routesLoaded.length) {
    routesLoaded[idx] = item;
  } else {
    routesLoaded.push(item);
  }
  $('routeVeil').classList.remove('on');
  renderModelRoutes();
  await saveRoutesConfig();
}

async function testRoute(route) {
  testModel(route.id, '', route.id + ' ➔ ' + route.target);
}

async function testModel(model, customKey, title) {
  $('testModelName').textContent = '· ' + (title || model);
  $('testStatus').hidden = false;
  $('testStatus').className = 'state';
  $('testStatus').innerHTML = '<span class="dots">正在向本地网关请求模型 ' + esc(model) + '</span>';
  $('testResult').hidden = true;
  $('testResult').textContent = '';
  $('testVeil').classList.add('on');

  const start = Date.now();
  try {
    const k = customKey || localStorage.getItem(LS_KEY) || '';
    const h = { 'Content-Type': 'application/json' };
    if (k) h['Authorization'] = 'Bearer ' + k;
    const res = await fetch('/v1/chat/completions', {
      method: 'POST',
      headers: h,
      body: JSON.stringify({
        model: model,
        messages: [{ role: 'user', content: 'Ping' }],
        max_tokens: 32
      })
    });
    const durMs = Date.now() - start;
    const body = await res.json().catch(() => ({}));
    if (!res.ok) {
      $('testStatus').className = 'state err';
      $('testStatus').textContent = '调用失败 (HTTP ' + res.status + '，耗时 ' + durMs + 'ms)';
      $('testResult').hidden = false;
      $('testResult').textContent = JSON.stringify(body, null, 2);
    } else {
      $('testStatus').className = 'state ok';
      const msg = body.choices && body.choices[0] && body.choices[0].message;
      const content = msg ? (msg.content || '') : '';
      const reason = msg && msg.reasoning_content ? ('\n[思考输出]\n' + msg.reasoning_content + '\n') : '';
      $('testStatus').textContent = '测试成功 (耗时 ' + durMs + 'ms, 上游 ' + (body.model || model) + ')';
      $('testResult').hidden = false;
      $('testResult').textContent = (reason ? reason + '\n[回复内容]\n' : '') + content + '\n\n完整响应:\n' + JSON.stringify(body, null, 2);
    }
  } catch (err) {
    $('testStatus').className = 'state err';
    $('testStatus').textContent = '请求异常: ' + err.message;
  }
}

/* ── API 令牌 ──────────────────────────────────────────────────────── */
let upstreamModelsCache = null;

async function fetchUpstreamModels(forceRefresh) {
  if (upstreamModelsCache && !forceRefresh) return upstreamModelsCache;
  try {
    const res = await api('upstream-models');
    if (res && res.models) {
      upstreamModelsCache = res.models;
      return upstreamModelsCache;
    }
  } catch (e) {
    console.warn('fetchUpstreamModels failed:', e);
  }
  return [];
}

async function populateTokenBindModelSelect(selectedModel, forceRefresh) {
  const sel = $('tokenBindModel');
  if (!sel) return;
  sel.innerHTML = '<option value="">正在从腾讯官方上游实时探测真实模型...</option>';
  const models = await fetchUpstreamModels(forceRefresh);
  if (!models || !models.length) {
    if (selectedModel) {
      sel.innerHTML = buildGroupedModelOptions([], selectedModel, '-- 请选择腾讯官方真实模型 (唯一绑定) --');
    } else {
      sel.innerHTML = '<option value="">(未能拉取到官方模型，请先检查账号池是否正常)</option>';
    }
    return;
  }
  sel.innerHTML = buildGroupedModelOptions(models, selectedModel, '-- 请选择腾讯官方真实模型 (唯一绑定) --');
}

function renderTokens() {
  const tb = $('tokensBody');
  if (!tb) return;
  if (!tokensLoaded || !tokensLoaded.length) {
    tb.innerHTML = '<tr><td colspan="6"><div class="empty">暂无 API 令牌，点击右上角「新建令牌」创建客户端专属 Key</div></td></tr>';
    return;
  }
  tb.innerHTML = tokensLoaded.map((t, i) => {
    const statusTag = t.enabled
      ? '<span class="tag ok">有效</span>'
      : '<span class="tag bad">已禁用</span>';
    const boundTag = t.bind_model
      ? '<span class="tag ok" style="font-weight:600;display:inline-flex;align-items:center;gap:4px;"><i class="dot ok"></i>' + esc(t.bind_model) + '</span>'
      : (t.models && t.models.length && !t.models.includes('*')
          ? t.models.map(m => '<span class="tag ok">' + esc(m) + '</span>').join(' ')
          : '<span class="tag">全部已启用模型 (*)</span>');
    const rpmTag = t.rpm > 0 ? (t.rpm + ' RPM') : '<span style="color:var(--ink-3)">默认</span>';
    const maskedKey = t.key ? (t.key.length > 10 ? t.key.slice(0, 7) + '...' + t.key.slice(-4) : t.key) : '—';
    return '<tr>' +
      '<td>' + statusTag + '</td>' +
      '<td><b>' + esc(t.name || '未命名') + '</b></td>' +
      '<td>' + boundTag + '</td>' +
      '<td class="num"><code style="font-size:12px;background:var(--surface-2);padding:2px 6px;border-radius:4px;cursor:pointer;" title="点击复制 Key" class="token-key-copy" data-key="' + esc(t.key) + '">' + esc(maskedKey) + '</code> <button type="button" class="xs ghost" data-act="copy-key" data-key="' + esc(t.key) + '">复制</button></td>' +
      '<td class="num">' + rpmTag + '</td>' +
      '<td class="acts">' +
        '<button type="button" class="xs ghost" data-act="copy-wb-cfg" data-idx="' + i + '" title="一键复制 WorkBuddy 实例配置参数">复制配置</button>' +
        '<button type="button" class="xs ghost" data-act="test-token" data-idx="' + i + '" title="测试该 Token 连通性">测试</button>' +
        '<button type="button" class="xs ghost" data-act="edit-token" data-idx="' + i + '">编辑</button>' +
        (t.enabled
          ? '<button type="button" class="xs ghost" data-act="toggle-token" data-idx="' + i + '">禁用</button>'
          : '<button type="button" class="xs primary" data-act="toggle-token" data-idx="' + i + '">启用</button>') +
        '<button type="button" class="xs ghost danger" data-act="del-token" data-idx="' + i + '">删除</button>' +
      '</td>' +
    '</tr>';
  }).join('');
}

async function loadTokens() {
  try {
    const d = await api('config');
    cfgLoaded = d.config;
    tokensLoaded = (cfgLoaded && cfgLoaded.tokens) ? cfgLoaded.tokens : [];
    renderTokens();
  } catch (e) {
    const tb = $('tokensBody');
    if (tb) tb.innerHTML = '<tr><td colspan="6"><div class="empty">加载令牌失败：' + esc(e.message) + '</div></td></tr>';
  }
}

async function saveTokensConfig() {
  try {
    if (cfgLoaded) {
      cfgLoaded.tokens = tokensLoaded;
    }
    const res = await api('config', { method: 'POST', body: JSON.stringify({ tokens: tokensLoaded }) });
    if (!res || res.ok === false) {
      throw new Error(res && res.error ? res.error : '保存失败');
    }
    toast('API 令牌已更新并立即生效', 'ok');
    const d = await api('config');
    cfgLoaded = d.config;
    tokensLoaded = (cfgLoaded && cfgLoaded.tokens) ? cfgLoaded.tokens : [];
    renderTokens();
  } catch (e) {
    toast('保存令牌失败: ' + e.message, 'err');
    try {
      const d = await api('config');
      cfgLoaded = d.config;
      tokensLoaded = (cfgLoaded && cfgLoaded.tokens) ? cfgLoaded.tokens : [];
      renderTokens();
    } catch (_) {}
  }
}

function generateRandomTokenKey() {
  const chars = '0123456789abcdef';
  let out = 'sk-';
  for (let i = 0; i < 32; i++) {
    out += chars[Math.floor(Math.random() * chars.length)];
  }
  $('tokenKey').value = out;
}

async function openTokenModal(idx, prefilledBindModel) {
  $('tokenEditIndex').value = String(idx);
  let curBind = prefilledBindModel || '';
  if (idx >= 0 && tokensLoaded && tokensLoaded[idx]) {
    const t = tokensLoaded[idx];
    $('tokenModalTitle').textContent = '编辑 API 令牌';
    $('tokenName').value = t.name || '';
    $('tokenKey').value = t.key || '';
    curBind = t.bind_model || '';
    $('tokenModels').value = (t.models || []).join(', ');
    $('tokenRPM').value = t.rpm || '';
    $('tokenEnabled').checked = t.enabled !== false;
  } else {
    $('tokenModalTitle').textContent = '新建 API 令牌';
    $('tokenName').value = curBind ? ('WorkBuddy-' + curBind) : '';
    generateRandomTokenKey();
    $('tokenModels').value = '';
    $('tokenRPM').value = '';
    $('tokenEnabled').checked = true;
  }
  $('tokenVeil').classList.add('on');
  await populateTokenBindModelSelect(curBind, false);
}

async function saveToken() {
  const name = $('tokenName').value.trim();
  const key = $('tokenKey').value.trim();
  if (!name) { $('tokenName').focus(); toast('请输入令牌名称', 'err'); return; }
  if (!key) { $('tokenKey').focus(); toast('请输入或生成 API Key', 'err'); return; }

  const bind_model = $('tokenBindModel') ? $('tokenBindModel').value.trim() : '';
  const models = $('tokenModels').value.split(/[,，\s]+/).map(s => s.trim()).filter(Boolean);
  const rpm = parseInt($('tokenRPM').value, 10) || 0;
  const enabled = $('tokenEnabled').checked;

  const item = {
    name,
    key,
    bind_model: bind_model || '',
    models: models.length ? models : (bind_model ? [bind_model] : ['*']),
    rpm,
    enabled
  };

  const idx = parseInt($('tokenEditIndex').value, 10);
  if (idx >= 0 && idx < tokensLoaded.length) {
    tokensLoaded[idx] = item;
  } else {
    tokensLoaded.push(item);
  }
  $('tokenVeil').classList.remove('on');
  renderTokens();
  await saveTokensConfig();
}

$('tblRoutes').onclick = async e => {
  const btn = e.target.closest('button[data-act]');
  if (!btn) return;
  const act = btn.dataset.act;
  // 模型级熔断手动恢复（无需 idx）
  if (act === 'circuit-revive') {
    const model = btn.dataset.model;
    if (!model) return;
    try {
      await api('circuit/revive', { method: 'POST', body: JSON.stringify({ model }) });
      toast('模型 ' + model + ' 已手动恢复为主模型', 'ok');
      await fetchCircuitStatus();
      renderModelRoutes();
    } catch (err) {
      toast('恢复失败: ' + err.message, 'err');
    }
    return;
  }
  const idx = Number(btn.dataset.idx);
  if (isNaN(idx) || !routesLoaded || !routesLoaded[idx]) return;
  if (act === 'toggle-route') {
    routesLoaded[idx].enabled = !routesLoaded[idx].enabled;
    renderModelRoutes();
    await saveRoutesConfig();
  } else if (act === 'edit-route') {
    openRouteModal(idx);
  } else if (act === 'del-route') {
    const ok = typeof confirm === 'function' ? confirm('确定删除模型路由「' + routesLoaded[idx].id + '」吗？') : true;
    if (!ok) return;
    routesLoaded.splice(idx, 1);
    renderModelRoutes();
    await saveRoutesConfig();
  } else if (act === 'test-route') {
    testRoute(routesLoaded[idx]);
  }
};

$('tblTokens').onclick = async e => {
  const btn = e.target.closest('button[data-act], code[data-key]');
  if (!btn) return;
  if (btn.dataset.act === 'copy-key' || btn.classList.contains('token-key-copy')) {
    const k = btn.dataset.key;
    if (k) {
      if (navigator.clipboard && navigator.clipboard.writeText) {
        navigator.clipboard.writeText(k).then(() => toast('API Key 已复制到剪贴板', 'ok')).catch(() => toast(k));
      } else {
        toast(k);
      }
    }
    return;
  }
  const act = btn.dataset.act;
  const idx = Number(btn.dataset.idx);
  if (isNaN(idx) || !tokensLoaded || !tokensLoaded[idx]) return;
  if (act === 'toggle-token') {
    tokensLoaded[idx].enabled = !tokensLoaded[idx].enabled;
    renderTokens();
    await saveTokensConfig();
  } else if (act === 'copy-wb-cfg') {
    const t = tokensLoaded[idx];
    const host = window.location.host || '127.0.0.1:9527';
    const text = `接口地址(Base URL): http://${host}/v1\nAPI Key: ${t.key}\n模型名称(Model): ${t.bind_model || 'default'}`;
    if (navigator.clipboard && navigator.clipboard.writeText) {
      navigator.clipboard.writeText(text).then(() => toast('已复制 WorkBuddy 实例配置参数', 'ok')).catch(() => toast(text));
    } else {
      toast(text);
    }
  } else if (act === 'test-token') {
    const t = tokensLoaded[idx];
    testModel(t.bind_model || 'default', t.key, (t.name || 'Token') + ' (' + (t.bind_model || 'default') + ')');
  } else if (act === 'edit-token') {
    openTokenModal(idx);
  } else if (act === 'del-token') {
    const tokenItem = tokensLoaded[idx];
    if (!tokenItem) return;
    const ok = typeof confirm === 'function' ? confirm('确定删除令牌「' + (tokenItem.name || tokenItem.key) + '」吗？') : true;
    if (!ok) return;
    tokensLoaded.splice(idx, 1);
    if (cfgLoaded) {
      cfgLoaded.tokens = tokensLoaded;
    }
    renderTokens();
    await saveTokensConfig();
  }
};

if ($('btnRefreshUpstreamModels')) {
  $('btnRefreshUpstreamModels').onclick = () => {
    populateTokenBindModelSelect($('tokenBindModel').value, true);
    toast('已触发刷新上游模型列表', 'ok');
  };
}

$('btnAddRoute').onclick = () => openRouteModal(-1);
$('btnCancelRoute').onclick = () => $('routeVeil').classList.remove('on');
$('btnSaveRoute').onclick = saveRoute;

// 故障转移降级模型交互控制
if ($('btnAddFallbackModel')) {
  $('btnAddFallbackModel').onclick = () => {
    const sel = $('routeFallbackSelect');
    const val = sel ? sel.value.trim() : '';
    if (!val) {
      toast('请先在下拉列表中选择一个上游真实模型', 'err');
      return;
    }
    const cur = ($('routeFallbacks').value || '').trim();
    const list = cur ? cur.split(/[,，\s]+/).map(s => s.trim()).filter(Boolean) : [];
    if (list.includes(val)) {
      toast('模型 ' + val + ' 已经在备选降级列表中了', 'err');
      return;
    }
    list.push(val);
    $('routeFallbacks').value = list.join(', ');
    renderRouteFallbackChips();
    toast('已将 ' + val + ' 加入备选降级列表', 'ok');
  };
}
if ($('btnClearFallbacks')) {
  $('btnClearFallbacks').onclick = () => {
    $('routeFallbacks').value = '';
    renderRouteFallbackChips();
  };
}
if ($('routeFallbacks')) {
  $('routeFallbacks').oninput = () => renderRouteFallbackChips();
}
if ($('routeFallbackChips')) {
  $('routeFallbackChips').onclick = e => {
    const a = e.target.closest('[data-rm-fb]');
    if (!a) return;
    const rmIdx = parseInt(a.dataset.rmFb, 10);
    const cur = ($('routeFallbacks').value || '').trim();
    const list = cur ? cur.split(/[,，\s]+/).map(s => s.trim()).filter(Boolean) : [];
    if (rmIdx >= 0 && rmIdx < list.length) {
      list.splice(rmIdx, 1);
      $('routeFallbacks').value = list.join(', ');
      renderRouteFallbackChips();
    }
  };
}

$('btnAddToken').onclick = () => openTokenModal(-1);
$('btnGenTokenKey').onclick = generateRandomTokenKey;
$('btnCancelToken').onclick = () => $('tokenVeil').classList.remove('on');
$('btnSaveToken').onclick = saveToken;
$('btnCloseTest').onclick = () => $('testVeil').classList.remove('on');
document.querySelectorAll('.route-preset').forEach(b => {
  b.onclick = () => {
    const t = b.dataset.target || '';
    $('routeId').value = b.dataset.id || '';
    $('routeTarget').value = t;
    if ($('routeTargetSelect')) $('routeTargetSelect').value = t;
    if (b.dataset.reasoning) $('routeReasoning').value = b.dataset.reasoning;
  };
});

async function loadModels() {
  const tb = $('mdBody');
  tb.innerHTML = '<tr><td colspan="8"><div class="empty">正在向上游查询…</div></td></tr>';
  try {
    // 探测数据是可选增强：拉取失败不影响模型列表本身
    const [d, pr] = await Promise.all([api('models'), api('model_probes').catch(() => ({}))]);
    const list = d.models || [];
    if (!list.length) { tb.innerHTML = '<tr><td colspan="8"><div class="empty">上游未返回模型</div></td></tr>'; return; }
    const probes = pr.probes || {};
    const probeKeys = Object.keys(probes);
    const probeOf = id => probes[id] || probes[probeKeys.find(k => k.endsWith(':' + id))];
    tb.innerHTML = list.map(m => {
      const rawId = m.raw_id || m.id.replace(/^(cn|global):/, '');
      const eff = (m.supported_efforts || []).slice();
      if (m.can_disable_thinking && eff.length && !eff.includes('off')) eff.push('off（可关）');
      const effs = eff.length ? eff.map(e => '<span class="tag warn">' + esc(e) + '</span>').join(' ')
        : '<span style="color:var(--ink-3);font-size:12.5px">' + (m.supports_reasoning ? '固定档 · 默认 ' + esc(m.default_effort || '?') : '不支持思考') + '</span>';
      // 能力徽标：默认模型 / 工具调用 / 视觉 / 纯推理（上游目录全字段透出，缺失不显示）
      const caps = [];
      if (m.is_alias) caps.push('<span class="tag ok">别名 ➔ ' + esc(m.target || '') + '</span>');
      if (m.is_default) caps.push('<span class="tag ok">默认</span>');
      if (m.supports_tool_call) caps.push('<span class="tag warn">工具</span>');
      if (m.supports_images) caps.push('<span class="tag warn">视觉</span>');
      if (m.supports_reasoning && !m.can_disable_thinking) caps.push('<span class="tag warn">思考常开</span>');
      const capHtml = caps.length ? '<div class="id" style="margin-top:2px">' + caps.join(' ') + '</div>' : '';
      const tip = m.description ? ' title="' + esc(m.description) + '"' : '';
      const acts = '<td class="acts">' +
        '<button type="button" class="xs ghost" data-act="quick-route" data-model="' + esc(rawId) + '" title="将此模型添加到模型路由规则">加路由</button> ' +
        '<button type="button" class="xs ghost" data-act="quick-token" data-model="' + esc(rawId) + '" title="为此模型生成独立专属 API Key">建Key</button>' +
        '</td>';
      return '<tr><td class="mark" aria-hidden="true"><i></i></td><td class="who"' + tip + '><div class="nm">' + esc(m.id) + '</div><div class="id">' + esc(m.name || '') + '</div>' + capHtml + '</td>' +
        '<td class="num">' + rateCell(m) + '</td>' +
        '<td>' + (m.default_effort ? '<span class="tag ok">' + esc(m.default_effort) + '</span>' : '<span style="color:var(--ink-3)">—</span>') + '</td>' +
        '<td class="efs" style="white-space:normal">' + effs + '</td>' +
        '<td class="num">' + (m.context_length ? Math.round(m.context_length / 1000) + 'K' : '—') + '</td>' +
        outCell(m, probeOf(m.id)) +
        acts +
        '</tr>';
    }).join('');
    const hit = list.filter(m => probeOf(m.id)).length;
    $('mdNote').textContent = list.length + ' 个模型 · 已刷新降级缓存' + (hit ? ' · ' + hit + ' 个有实测上限' : '');
  } catch (e) {
    tb.innerHTML = '<tr><td colspan="8"><div class="empty">' + esc(e.message) + '</div></td></tr>';
  }
}
$('btnModels').onclick = loadModels;

if ($('tblModels')) {
  $('tblModels').onclick = e => {
    const btn = e.target.closest('button[data-act]');
    if (!btn) return;
    const act = btn.dataset.act;
    const model = btn.dataset.model;
    if (act === 'quick-route') {
      openRouteModal(-1, model);
    } else if (act === 'quick-token') {
      go('tokens');
      openTokenModal(-1, model);
    }
  };
}

/* ── 日志（频道：全部/任务/对话/系统） ─────────────────────────────── */
let logCh = 'all';
$('logChips').addEventListener('click', ev => {
  const b = ev.target.closest('button[data-ch]');
  if (!b) return;
  logCh = b.dataset.ch;
  document.querySelectorAll('#logChips .chip').forEach(c => c.classList.toggle('on', c === b));
  loadLogs();
});
async function loadLogs() {
  const box = $('logBox');
  const atEnd = box.scrollTop + box.clientHeight >= box.scrollHeight - 24;
  try {
    const d = await api('logs');
    const entries = (d.entries || []).filter(e => logCh === 'all' || e.ch === logCh);
    box.innerHTML = entries.length
      ? entries.map(e => {
        const lvl = /error|失败|错误/.test(e.text) ? ' e' : /warn|冷却|熔断/.test(e.text) ? ' w' : '';
        const t = e.ts ? new Date(e.ts).toLocaleTimeString('zh-CN', { hour12: false }) : '';
        const ch = logCh === 'all' ? '<i class="lch c-' + esc(e.ch) + '">' + ({ task: '任务', chat: '对话', sys: '系统' }[e.ch] || e.ch) + '</i>' : '';
        return '<span class="ln' + lvl + '">' + ch + esc(t + ' ' + e.text) + '</span>';
      }).join('')
      : '<span style="color:var(--ink-3)">暂无日志</span>';
    if (logPin && atEnd) box.scrollTop = box.scrollHeight;
    const counts = {};
    for (const e of (d.entries || [])) counts[e.ch] = (counts[e.ch] || 0) + 1;
    $('logNote').textContent = logCh === 'all'
      ? '任务 ' + (counts.task || 0) + ' · 对话 ' + (counts.chat || 0) + ' · 系统 ' + (counts.sys || 0)
      : (logCh === 'task' ? '任务' : logCh === 'chat' ? '对话' : '系统') + ' ' + entries.length + ' 行';
  } catch (e) { /* 概览已提示 */ }
}
$('btnLogPin').onclick = () => {
  logPin = !logPin;
  $('btnLogPin').textContent = '自动滚动：' + (logPin ? '开' : '关');
};

/* ── 配置 ─────────────────────────────────────────────────────────── */
const CFG_MAP = {
  listen: ['listen'], port: ['port'], api_key: ['api_key'],
  package_detail_limit: ['panel', 'package_detail_limit'],
  checkin_hours: ['schedule', 'checkin_hours'], checkin_enabled: ['schedule', 'checkin_enabled'], growth_hours: ['schedule', 'growth_hours'], growth_enabled: ['schedule', 'growth_enabled'],
  travel_hours: ['schedule', 'travel_hours'], travel_enabled: ['schedule', 'travel_enabled'],
  activity_hours: ['schedule', 'activity_hours'], activity_enabled: ['schedule', 'activity_enabled'],
  keepalive_hours: ['schedule', 'keepalive_hours'], keepalive_enabled: ['schedule', 'keepalive_enabled'],
  balance_refresh_enabled: ['schedule', 'balance_refresh_enabled'], balance_refresh_minutes: ['schedule', 'balance_refresh_minutes'],
  max_in_flight: ['pool', 'max_in_flight'], max_in_flight_global: ['pool', 'max_in_flight_global'],
  breaker_threshold: ['pool', 'breaker_threshold'],
  degrade_threshold: ['pool', 'degrade_threshold'], degrade_cooldown: ['pool', 'degrade_cooldown'],
  degrade_cooldown_max: ['pool', 'degrade_cooldown_max'],
  cost_explore_interval: ['pool', 'cost_explore_interval'],
  prefer_expiring: ['pool', 'prefer_expiring'], expiring_soon: ['pool', 'expiring_soon'],
  soft_rate: ['cooldown', 'soft_rate'], soft_rate_max: ['cooldown', 'soft_rate_max'],
  breaker_cooldown: ['pool', 'breaker_cooldown'], breaker_cooldown_max: ['pool', 'breaker_cooldown_max'],
  idle_weight_per_hour: ['pool', 'idle_weight_per_hour'], idle_weight_max: ['pool', 'idle_weight_max'],
  ttl: ['session_sticky', 'ttl'],
  timeout_seconds: ['upstream', 'timeout_seconds'], header_timeout_seconds: ['upstream', 'header_timeout_seconds'],
  idle_timeout_seconds: ['upstream', 'idle_timeout_seconds'], user_agent: ['upstream', 'user_agent'],
  prompt_mode: ['prompt', 'mode'], prompt_file: ['prompt', 'file'],
  sanitize_blacklist_fingerprints: ['features', 'sanitize_blacklist_fingerprints'],
  session_sticky_enabled: ['session_sticky', 'enabled'],
};
function dig(obj, path) { return path.reduce((o, k) => (o == null ? undefined : o[k]), obj); }
function put(obj, path, val) {
  let o = obj;
  for (let i = 0; i < path.length - 1; i++) { if (typeof o[path[i]] !== 'object' || o[path[i]] === null) o[path[i]] = {}; o = o[path[i]]; }
  o[path[path.length - 1]] = val;
}

async function loadConfig() {
  try {
    const d = await api('config');
    cfgLoaded = d.config;
    $('cfgPath').textContent = d.path || '';
    const f = $('cfgForm');
    for (const [name, path] of Object.entries(CFG_MAP)) {
      const el = f.elements[name];
      if (!el) continue;
      const v = dig(cfgLoaded, path);
      if (el.type === 'checkbox') el.checked = !!v;
      else if (Array.isArray(v)) el.value = v.join(', ');
      else el.value = v == null ? '' : v;
    }
    const modelsEl = f.elements['models_json'];
    if (modelsEl) {
      const m = cfgLoaded.models;
      modelsEl.value = m && Object.keys(m).length ? JSON.stringify(m, null, 2) : '';
    }
    markDurationFields(); // 回填后重置校验态（清掉残留红框；现值来自后端必然合法）
    $('cfgNote').textContent = '';
  } catch (e) { toast('读取配置失败：' + e.message, 'err'); }
}
function collectConfig() {
  const f = $('cfgForm'), out = {};
  for (const [name, path] of Object.entries(CFG_MAP)) {
    const el = f.elements[name];
    if (!el) continue;
    let v;
    if (el.type === 'checkbox') v = el.checked;
    else if (el.type === 'number') { v = el.value.trim() === '' ? undefined : Number(el.value); }
    else {
      const raw = el.value.trim();
      if (raw === '') v = undefined;
      else if (name.endsWith('_hours')) {
        try {
          v = parseHours(raw);
        } catch (_) {
          v = raw.split(/[,，\s]+/).filter(Boolean).map(Number);
        }
      }
      else v = raw;
    }
    if (v !== undefined) put(out, path, v);
  }
  const modelsEl = f.elements['models_json'];
  if (modelsEl) {
    const raw = modelsEl.value.trim();
    if (raw) {
      try {
        out.models = JSON.parse(raw);
      } catch (e) {}
    } else {
      out.models = {};
    }
  }
  if (Array.isArray(tokensLoaded) && tokensLoaded.length > 0) {
    out.tokens = tokensLoaded;
  } else if (cfgLoaded && Array.isArray(cfgLoaded.tokens)) {
    out.tokens = cfgLoaded.tokens;
  }
  if (Array.isArray(routesLoaded) && routesLoaded.length > 0) {
    out.model_routes = routesLoaded;
  } else if (cfgLoaded && Array.isArray(cfgLoaded.model_routes)) {
    out.model_routes = cfgLoaded.model_routes;
  }
  return out;
}

/* 时点字段即时校验与解析 (0..23 整点) */
const HOUR_FIELDS = ['checkin_hours', 'growth_hours', 'travel_hours', 'activity_hours', 'keepalive_hours'];

function parseHours(raw) {
  if (!raw || !raw.trim()) return [];
  const parts = raw.trim().split(/[,，\s]+/).filter(Boolean);
  const nums = [];
  for (const p of parts) {
    if (!/^\d+$/.test(p)) {
      throw new Error(`包含非数字字符 "${p}"，时点必须为 0~23 的整数`);
    }
    const n = Number(p);
    if (!Number.isInteger(n) || n < 0 || n > 23) {
      throw new Error(`时点 "${p}" 超出范围，必须在 0 到 23 之间`);
    }
    nums.push(n);
  }
  return nums;
}

function focusField(el) {
  if (!el) return;
  const parentDetails = el.closest('details');
  if (parentDetails) parentDetails.open = true;
  el.focus();
}

/* Go 时长字段即时校验：空 = 沿用现值（collectConfig 跳过发送）；非空必须是
   ParseDuration 语法（30m / 2h / 600s / 1h30m，可组合可带小数）。与后端
   config.go normalize() 的 time.ParseDuration 同口径，脏值在前端就地标红，
   不再等到保存被拒。 */
const DURATION_RE = /^(\d+(\.\d+)?(ns|us|µs|ms|s|m|h))+$/;
const DURATION_FIELDS = ['soft_rate', 'soft_rate_max', 'breaker_cooldown', 'breaker_cooldown_max',
  'degrade_cooldown', 'degrade_cooldown_max', 'cost_explore_interval', 'expiring_soon', 'ttl'];
const DURATION_TIP = '格式应为 Go 时长：30m / 2h / 600s / 1h30m';
function durationBad(name) {
  const el = $('cfgForm').elements[name];
  if (!el) return false;
  const v = el.value.trim();
  return v !== '' && !DURATION_RE.test(v);
}
function markDurationFields() {
  for (const name of DURATION_FIELDS) {
    const el = $('cfgForm').elements[name];
    if (!el) continue;
    const bad = durationBad(name);
    el.classList.toggle('invalid', bad);
    el.title = bad ? DURATION_TIP : '';
  }
}
$('cfgForm').addEventListener('input', ev => {
  if (DURATION_FIELDS.includes(ev.target.name)) markDurationFields();
  if (HOUR_FIELDS.includes(ev.target.name)) {
    const el = ev.target;
    if (el.value.trim()) {
      try {
        parseHours(el.value);
        el.classList.remove('invalid');
        el.title = '';
      } catch (err) {
        el.classList.add('invalid');
        el.title = err.message;
      }
    } else {
      el.classList.remove('invalid');
      el.title = '';
    }
  }
});
$('btnEye').onclick = () => {
  const el = $('cfgKey');
  const show = el.type === 'password';
  el.type = show ? 'text' : 'password';
  $('btnEye').textContent = show ? '隐藏' : '显示';
};
$('btnCfgReload').onclick = loadConfig;
$('cfgForm').onsubmit = async ev => {
  ev.preventDefault();
  // 时点字段格式与范围校验：0..23 整数
  for (const name of HOUR_FIELDS) {
    const el = $('cfgForm').elements[name];
    if (!el || !el.value.trim()) continue;
    try {
      parseHours(el.value);
      el.classList.remove('invalid');
    } catch (err) {
      focusField(el);
      el.classList.add('invalid');
      const label = el.closest('.fld')?.querySelector('.lb')?.textContent || name;
      toast(`「${label}」${err.message}`, 'err');
      return;
    }
  }
  // 时长字段脏值拦截：标红 + toast 点名，不发保存请求（后端同样会拒，这里前置）。
  markDurationFields();
  const firstBad = DURATION_FIELDS.find(durationBad);
  if (firstBad) {
    const el = $('cfgForm').elements[firstBad];
    focusField(el);
    toast('「' + (el.closest('.fld')?.querySelector('.lb')?.textContent || firstBad) + '」' + DURATION_TIP, 'err');
    return;
  }
  const modelsEl = $('cfgForm').elements['models_json'];
  if (modelsEl && modelsEl.value.trim()) {
    try {
      const parsed = JSON.parse(modelsEl.value.trim());
      if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) {
        throw new Error('必须是 JSON 对象（如 {"别名": "上游模型"}）');
      }
    } catch (e) {
      focusField(modelsEl);
      toast('模型映射 JSON 格式无效：' + e.message, 'err');
      return;
    }
  }
  const btn = $('btnCfgSave');
  btn.disabled = true; btn.textContent = '保存中…';
  try {
    const r = await api('config', { method: 'POST', body: JSON.stringify(collectConfig()) });
    const n = (r.restart_required || []).length;
    toast(n ? '配置已保存，其中 ' + n + ' 项需重启进程生效' : '配置已保存并立即生效', 'ok');
    // 密钥可能已改：本次会话沿用新值，避免下一次轮询被 401。
    const k = $('cfgKey').value.trim();
    if (k) localStorage.setItem(LS_KEY, k);
    loadConfig();
    loadOverview(true);
  } catch (e) { toast('保存失败：' + e.message, 'err'); }
  finally { btn.disabled = false; btn.textContent = '保存配置'; }
};

// 快捷预设药丸按钮交互
document.addEventListener('click', ev => {
  const btn = ev.target.closest('.pill-btn');
  if (!btn) return;
  const inputName = btn.dataset.input;
  const switchName = btn.dataset.switch;
  const val = btn.dataset.val;
  if (!inputName) return;

  const f = $('cfgForm');
  const input = f ? f.elements[inputName] : null;
  if (input) {
    input.value = val;
    input.dispatchEvent(new Event('input', { bubbles: true }));
    input.dispatchEvent(new Event('change', { bubbles: true }));
  }
  if (switchName && f) {
    const sw = f.elements[switchName];
    if (sw && !sw.checked) {
      sw.checked = true;
      sw.dispatchEvent(new Event('change', { bubbles: true }));
    }
  }
  const btnText = btn.textContent.trim();
  toast(`已设定预设：${btnText}`, 'ok');
});

/* ── 添加账号 ─────────────────────────────────────────────────────── */
function openAdd() {
  $('addVeil').classList.add('on');
  // 重置到登录标签
  switchAddTab('login');
  $('addPick').hidden = false;
  $('addLoad').hidden = true; $('addReady').hidden = true;
  $('addDone').hidden = true; $('addErr').hidden = true;
  $('importDone').hidden = true; $('importErr').hidden = true;
  $('btnCopyUrl').hidden = true; $('btnOpenUrl').hidden = true;
  $('btnStartLogin').hidden = false; $('btnStartLogin').disabled = false;
  stopPoll();
}
function switchAddTab(tab) {
  document.querySelectorAll('#addTabs .tab').forEach(b => b.classList.toggle('on', b.dataset.tab === tab));
  $('addTabLogin').hidden = tab !== 'login';
  $('addTabImport').hidden = tab !== 'import';
}
document.querySelectorAll('#addTabs .tab').forEach(b => {
  b.onclick = () => switchAddTab(b.dataset.tab);
});
function startAddLogin() {
  const realm = (document.querySelector('input[name="addRealm"]:checked') || {}).value || 'cn';
  $('btnStartLogin').disabled = true;
  $('addLoad').hidden = false; $('addErr').hidden = true;
  api('login/start', { method: 'POST', body: JSON.stringify({ realm }) }).then(r => {
    loginState = r.state;
    $('addUrl').textContent = r.url;
    $('addPick').hidden = true; // 选域锁定（会话已按该域发起）
    $('addLoad').hidden = true; $('addReady').hidden = false;
    $('btnStartLogin').hidden = true;
    $('btnCopyUrl').hidden = false; $('btnOpenUrl').hidden = false;
    loginTimer = setInterval(pollLogin, 3000);
  }).catch(e => {
    $('addLoad').hidden = true;
    $('btnStartLogin').disabled = false;
    $('addErr').hidden = false;
    $('addErr').textContent = e.message;
  });
}
function stopPoll() { if (loginTimer) { clearInterval(loginTimer); loginTimer = null; } }
async function pollLogin() {
  if (!loginState) return;
  try {
    const r = await api('login/poll?state=' + encodeURIComponent(loginState));
    if (r.done) {
      stopPoll();
      $('addReady').hidden = true;
      $('addDone').hidden = false;
      $('addDone').textContent = '已添加 ' + (r.nickname || r.uid) + (r.realm === 'global' ? '（国际版）' : '') + (r.credits >= 0 ? ' · 积分 ' + r.credits + (r.credits_total > 0 ? '/' + r.credits_total : '') : '') + '，账号已载入池中';
      setTimeout(() => { closeAdd(); loadOverview(true); }, 1600);
    }
  } catch (e) {
    stopPoll();
    $('addReady').hidden = true;
    $('addErr').hidden = false;
    $('addErr').textContent = e.message + '（关闭后重新添加）';
  }
}
function closeAdd() { stopPoll(); loginState = null; $('addVeil').classList.remove('on'); }
$('btnCloseAdd').onclick = closeAdd;
$('btnStartLogin').onclick = startAddLogin;
$('btnOpenUrl').onclick = () => open($('addUrl').textContent, '_blank');
$('btnCopyUrl').onclick = () => navigator.clipboard.writeText($('addUrl').textContent)
  .then(() => toast('链接已复制', 'ok'), () => toast('复制失败，请手动选择复制', 'err'));
$('importFile').onchange = async () => {
  const file = $('importFile').files[0];
  if (!file) return;
  $('importDone').hidden = true; $('importErr').hidden = true;
  const fd = new FormData();
  fd.append('file', file);
  const h = {};
  const k = localStorage.getItem(LS_KEY);
  if (k) h['Authorization'] = 'Bearer ' + k;
  try {
    const r = await fetch('/panel/api/import/cockpit', { method: 'POST', body: fd, headers: h });
    const d = await r.json();
    if (!r.ok) throw new Error(d.error || ('HTTP ' + r.status));
    $('importDone').hidden = false;
    $('importDone').textContent = '导入完成：成功 ' + d.imported + ' 个' + (d.skipped ? '，跳过 ' + d.skipped + ' 个' : '');
    if (d.errors && d.errors.length) {
      console.warn('import errors:', d.errors);
    }
    loadOverview(true);
  } catch (e) {
    $('importErr').hidden = false;
    $('importErr').textContent = '导入失败：' + e.message;
  }
  $('importFile').value = '';
};

/* ── 顶部动作 ─────────────────────────────────────────────────────── */
$('btnAdd').onclick = openAdd;
$('btnRefresh').onclick = async () => {
  const b = $('btnRefresh');
  b.disabled = true; b.textContent = '刷新中…';
  try {
    await api('balance_all', { method: 'POST' });
    await loadOverview(true);
    toast('余额已从上游刷新', 'ok');
  } catch (e) { toast('刷新失败：' + e.message, 'err'); await loadOverview(true); }
  finally { b.disabled = false; b.textContent = '刷新'; }
  if (view === 'logs') loadLogs();
};

/* ── 轮询 ─────────────────────────────────────────────────────────── */
function refreshVisible() {
  if (view === 'accounts') loadOverview(true);
  else if (view === 'logs') loadLogs();
  else if (view === 'taskscenter') reattachQueueView();
}
function start() {
  loadOverview(true);
  if (refTimer) clearInterval(refTimer);
  refTimer = setInterval(refreshVisible, 5000);
  checkAuthGate();
}
async function checkAuthGate() {
  try { await api('overview'); }
  catch (e) { if (String(e.message).includes('密钥') || String(e.message).includes('api_key')) return; }
}
start();

/* ── 积分任务 ─────────────────────────────────────────────────────── */
let taskUID = null;

// 可自动完成的任务（与后端 autoActions 表一致）：判据为行为事件、可经网关复现。
// 其余任务需在官方客户端内交互，面板只展示指引（行 title 提示）。
// 注意：键含点号（Model_chat_GLM5.2）必须加引号，否则会被解析成属性访问 + 数字字面量。
const AUTO_TASKS = {
  'chat_5': '上报 5 条对话活跃事件（自动补足差额）',
  'first_buddy': '上报解锁 → 同意协议 → 领取第一只 Buddy',
  'Model_chat_GLM5.2': '接受任务 → glm-5.2 真实对话一次 → 对齐模型上报',
  'RichMeow_Chat': '桌面指纹事件链上报（已验证可点亮）',
  'Buddy_App': '上报「进入 Buddy 应用」事件链（已验证可点亮）',
  'Buddy_App_QQ': '上报「进入企鹅教师助手」事件链（已验证可点亮）',
  'automation_1': '上报「定时任务创建」事件（已验证可点亮）',
  'Library_read': '上报「读资料库介绍」事件（已验证可点亮）',
  'template_5': '上报「使用模板创建任务」事件组 ×5（三账号实测点亮）',
  'playbook_prompt': '上报「灵感案例做同款发送 Prompt」事件组（三账号实测点亮）',
  'create_canvas': '上报「设计创意画布创建」事件组（三账号实测点亮，+300 分）',
  'expert_5': '真实专家召唤+使用链 ×5（专家市场+真实 chat，三账号实测点亮）',
  'Expert_team_use_3': '真实专家团召唤+使用链 ×3（三账号实测点亮）',
  'Hp_Appearance': '设置主题 API + 皮肤生效事件（两账号实测点亮）',
  'black_cat': '夜猫子：23:00–08:00 窗口内 glm-5.2 对话补足（窗口外提示等 23 点排程）',
  'Expert_lighthouse': '真实轻量云专家召唤+使用链（真实对话 requestId，两账号实测点亮）',
  'skill_1': '真实对话 + skill_info 技能加载事件（实测点亮）',
  'school_season': '校园日（小程序口径）：accept → mini 对话+activityId 上报 → 领奖（+100c+5e）',
  'Sequential_Tasks_1': '小程序首对话（小程序口径）：accept → mini 对话上报 → 领奖（+100c+5e）',
  'Sequential_Tasks_2': '小程序选专家对话（小程序口径）：市场专家 id → accept → expert_actual_use 上报 → 领奖（+200c+5e）',
  'Sequential_Tasks_3': '小程序五次对话（小程序口径）：accept → mini 对话上报 ×5（自动补差额）→ 领奖（+300c+5e）',
  'Sequential_Tasks_4': '小程序定时任务（预留，每日零点解锁一环）：accept → 定时任务创建事件 → 领奖（判据待解锁验证）',
  'Sequential_Tasks_5': '小程序使用 GLM5.2（预留）：accept → 带模型字段的 mini 对话上报 → 领奖（判据待解锁验证）',
  'Sequential_Tasks_6': '小程序十次对话（预留）：accept → mini 对话上报 ×target（自动补差额）→ 领奖',
  'Sequential_Tasks_7': '体验灵感功能（预留，疑 PC 口径）：accept → 灵感事件组（PC+mp 双形态）→ 领奖（判据待解锁验证）'
};

function openTasks(uid) {
  taskUID = uid;
  $('taskWho').textContent = uid.slice(0, 16);
  $('taskVeil').classList.add('on');
  $('btnTaskReload').hidden = false;
  loadTasks();
}
function closeTasks() { $('taskVeil').classList.remove('on'); taskUID = null; }
$('btnCloseTask').onclick = closeTasks;
$('btnTaskReload').onclick = loadTasks;

// 全部接受：把该账号未接受的任务一次性报名（幂等，跳过已接受/已领取）。
$('btnTaskAcceptAll').onclick = async () => {
  if (!taskUID) return;
  const btn = $('btnTaskAcceptAll');
  btn.disabled = true; btn.textContent = '接受中…';
  try {
    const r = await api('accounts/' + encodeURIComponent(taskUID) + '/tasks/accept_all', { method: 'POST' });
    const n = r.accepted || 0;
    if (r.failed && r.failed.length) {
      toast(`已接受 ${n} 个，${r.failed.length} 个被上游拒绝（可重试）`, 'err');
    } else {
      toast(n ? `已接受 ${n} 个任务` : (r.message || '所有任务均已接受'), 'ok');
    }
  } catch (e) { toast(e.message, 'err'); }
  finally { btn.disabled = false; btn.textContent = '全部接受'; loadTasks(); }
};

// 一键完成全部可自动任务（耗时较长：含真实对话，逐项回读验证）。
$('btnTaskAutoAll').onclick = async () => {
  if (!taskUID) return;
  const btn = $('btnTaskAutoAll');
  if (!confirm('将依次执行：补报对话事件、领取 Buddy、glm-5.2 对话、尝试上报。\n过程约 1-2 分钟（含真实对话），确认继续？')) return;
  btn.disabled = true; btn.textContent = '执行中…';
  try {
    const r = await api('accounts/' + encodeURIComponent(taskUID) + '/tasks/auto_all', { method: 'POST' });
    const okN = (r.results || []).filter(x => x.status === 'done').length;
    const skipN = (r.results || []).filter(x => x.status === 'skipped').length;
    const errN = (r.results || []).filter(x => x.status === 'error').length;
    toast(`执行完成：成功 ${okN} 项，跳过 ${skipN} 项${errN ? '，失败 ' + errN + ' 项' : ''}`, errN ? 'err' : 'ok');
    console.log('auto_all results:', r.results);
  } catch (e) { toast(e.message, 'err'); }
  finally { btn.disabled = false; btn.textContent = '一键完成可自动任务'; loadTasks(); }
};

async function loadTasks() {
  if (!taskUID) return;
  const st = $('taskState'), tb = $('taskTable');
  st.hidden = false;
  st.className = 'state';
  st.innerHTML = '<span class="dots">查询中</span>';
  tb.hidden = true;
  try {
    const d = await api('accounts/' + encodeURIComponent(taskUID) + '/tasks');
    const list = d.tasks || [];
    if (!list.length) {
      st.className = 'state';
      st.textContent = '该账号暂无任务';
      return;
    }
    // 有进度或可领取的排前面，已领取沉底——一眼看到"现在该做什么"。
    list.sort((a, b) => (a.claimed - b.claimed) || (b.claimable - a.claimable) || String(a.task_code).localeCompare(String(b.task_code)));
    $('taskBody').innerHTML = list.map(t => {
      // 进度：current 可能缺失（0 或被上游省略）——用 ?? 兜底，避免渲染成 "undefined / N"
      const cur = t.current ?? 0, tgt = t.target ?? 0;
      const prog = tgt ? cur + ' / ' + tgt : (tgt === 0 && cur > 0 ? String(cur) : '—');
      const parts = [];
      if (t.credit) parts.push('+' + t.credit + ' 分');
      if (t.energy) parts.push('+' + t.energy + ' 能');
      if (t.reward_buddy) parts.push('Buddy');
      const reward = parts.length ? parts.join(' ') : '—';
      const badge = t.claimed ? '<span class="tag ok">已领取</span>'
        : t.claimable ? '<span class="tag warn">可领取</span>'
        : t.locked ? '<span class="tag mute">未解锁</span>'
        : t.accept_status === 'accepted' ? '<span class="tag mute">进行中</span>'
        : '<span class="tag mute">未接受</span>';
      const acted = t.claimed || t.locked ? ''
        : t.claimable ? '<button class="xs primary" data-t="claim" data-c="' + esc(t.task_code) + '">领取</button>'
        : AUTO_TASKS[t.task_code] ? '<button class="xs primary" data-t="auto" data-c="' + esc(t.task_code) + '" title="' + esc(AUTO_TASKS[t.task_code]) + '">一键完成</button>'
        : t.accept_status === 'accepted' ? ''
        : '<button class="xs" data-t="accept" data-c="' + esc(t.task_code) + '">接受</button>';
      // 操作指引（description/task_desc）挂 title 提示：如何完成交给用户看
      const tip = [t.title, t.task_desc || t.description, t.jump_url ? '跳转：' + t.jump_url : ''].filter(Boolean).join('\n');
      return '<tr title="' + esc(tip) + '"><td class="mark" aria-hidden="true"><i></i></td>' +
        '<td class="who"><div class="nm">' + esc(t.title || t.task_code) + '</div><div class="id">' + esc(t.task_code) + (t.tag ? ' · ' + esc(t.tag) : '') + '</div></td>' +
        '<td class="num">' + esc(prog) + '</td>' +
        '<td class="num">' + esc(reward) + '</td>' +
        '<td>' + badge + '</td>' +
        '<td class="acts">' + acted + '</td></tr>';
    }).join('');
    st.hidden = true;
    tb.hidden = false;
  } catch (e) {
    st.className = 'state err';
    st.textContent = e.message;
  }
}

$('taskBody').addEventListener('click', async ev => {
  const b = ev.target.closest('button[data-t]');
  if (!b || !taskUID) return;
  const kind = b.dataset.t, code = b.dataset.c;
  b.disabled = true;
  try {
    if (kind === 'auto') {
      // 一键完成：后端执行动作 → 回读进度 → 汇报（耗时可到分钟级，含真实对话）
      b.textContent = '执行中…';
      const r = await api('accounts/' + encodeURIComponent(taskUID) + '/tasks/auto', {
        method: 'POST', body: JSON.stringify({ task_code: code })
      });
      if (r.skipped) {
        toast(r.message || '已跳过', 'ok');
      } else {
        const advanced = r.progress_before !== r.progress_after;
        let msg = r.message || '已执行';
        if (r.progress_after) msg += `（进度 ${r.progress_before} → ${r.progress_after}）`;
        if (r.claimed) msg += '，奖励已自动到账';
        else if (r.claimable) msg += r.claim_error ? '，可点「领取」重试' : '';
        else if (r.attempt && !advanced) msg += '；进度未动，该任务可能需要官方客户端';
        toast(msg, (r.claimed || advanced) ? 'ok' : 'err');
      }
      loadOverview(true);
    } else {
      const path = 'accounts/' + encodeURIComponent(taskUID) + '/tasks/' + (kind === 'claim' ? 'claim' : 'accept');
      const body = kind === 'claim' ? { task_code: code } : { task_codes: [code] };
      await api(path, { method: 'POST', body: JSON.stringify(body) });
      toast(kind === 'claim' ? '已领取奖励' : '已接受任务', 'ok');
      if (kind === 'claim') loadOverview(true);
    }
  } catch (e) { toast(e.message, 'err'); }
  finally { loadTasks(); }
});

/* ── 任务中心：开学季 + 全账号扫描/队列 ──────────────────────────── */
// 开学季任务单元：✓ 已领（绿）｜◐ x/y 进行中（琥珀）｜○ 未做（灰）
function staskHTML(t) {
  if (!t) return '<span class="stask todo"><span class="mark">·</span>—</span>';
  if (t.status === 'claimed') return '<span class="stask ok"><span class="mark">✓</span>已领</span>';
  if (t.status === 'completed') return '<span class="stask warn"><span class="mark">◆</span>可领</span>';
  if (t.status === 'in_progress') {
    const fr = t.target_count ? '<span class="fr">' + t.progress + '/' + t.target_count + '</span>' : '';
    return '<span class="stask warn"><span class="mark">◐</span>' + fr + '</span>';
  }
  return '<span class="stask todo"><span class="mark">○</span>未做</span>';
}
const LUCK_SVG = '<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.4"><path d="M3.2 5.2 5 1.8l3 2.4 3-2.4 1.8 3.4-1.4 2.6 1.4 2.6-3.4 2.2H6l-3.4-2.2 1.4-2.6z" opacity=".9"/><circle cx="8" cy="9" r="1.1" fill="currentColor" stroke="none"/></svg>';
const SCHOOL_TITLES = {
  share_invite: '分享活动 +100c', desktop_chat_1_time: '桌面端体验 +100c（单次）',
  chat_3_times: '和 AI 对话 3 次 +50c', expert_use: '召唤开学季专家 +50c',
  task_student_verify: '学生认证 +100c（需真实认证，不做）',
};

/* ── 精简 QR 编码器（券码二维码用）────────────────────────────────────
   规格子集：byte 模式、ECC L、版本 1-5（全部单纠错块，免块交织）、固定掩码 0。
   完整性：规范允许任选掩码（解码器按格式信息位自行去掩码），固定掩码不影响
   可扫描性；已用 python qrcode 库对多输入多版本做逐像素交叉验证（强制 byte
   模式 + mask 0，5/5 全部 diff=0）。面板 CSP 只允许 self，外链 QR 服务不可用。 */
// qr_gen.js —— 精简 QR 编码器（浏览器用 + node 可跑交叉验证）
// 规格子集：byte 模式、ECC L、版本 1-5（全部单纠错块，免块交织）、固定掩码 0。
// 完整性说明：规范允许编码器任选掩码（解码器按格式信息位自行去掩码），
// 固定掩码不影响可扫描性；券码为短文本，v1-5（26 字节起）绰绰有余。

// GF(256) 对数/指数表（本原多项式 0x11d）
const QR_EXP = new Array(512), QR_LOG = new Array(256);
(() => {
  let x = 1;
  for (let i = 0; i < 255; i++) { QR_EXP[i] = x; QR_LOG[x] = i; x <<= 1; if (x & 0x100) x ^= 0x11d; }
  for (let i = 255; i < 512; i++) QR_EXP[i] = QR_EXP[i - 255];
})();
const gmul = (a, b) => (a && b) ? QR_EXP[QR_LOG[a] + QR_LOG[b]] : 0;

// 各版本参数（下标 = 版本-1）：[数据码字数, 纠错码字数]，ECC L 单块
const QR_V = [[19, 7], [34, 10], [55, 15], [80, 20], [108, 26]];
// 对齐图案中心坐标（v2+；与定位图案重叠的位置在放置时跳过）
const QR_ALIGN = [[], [6, 18], [6, 22], [6, 26], [6, 30]];
const QR_MASK = (r, c) => (r + c) % 2 === 0; // 掩码模式 0

// 生成多项式（最高次系数在前，g[0] 恒为 1）
function qrGenPoly(deg) {
  let g = [1];
  for (let i = 0; i < deg; i++) {
    const a = QR_EXP[i], ng = new Array(g.length + 1).fill(0);
    ng[0] = g[0];
    for (let j = 1; j < g.length; j++) ng[j] = g[j] ^ gmul(a, g[j - 1]);
    ng[g.length] = gmul(a, g[g.length - 1]);
    g = ng;
  }
  return g;
}

// Reed-Solomon 求余（综合除法），返回 deg 个纠错码字
function rsRem(data, deg) {
  const g = qrGenPoly(deg);
  const res = data.concat(new Array(deg).fill(0));
  for (let i = 0; i < data.length; i++) {
    const f = res[i];
    if (f) for (let j = 0; j < g.length; j++) res[i + j] ^= gmul(g[j], f);
  }
  return res.slice(data.length);
}

// 文本 → 码字流（byte 模式：0100 + 8 位计数 + 数据 + 终止符 + 0xEC/0x11 填充）
function qrDataCodewords(text, dataCap) {
  const bytes = Array.from(new TextEncoder().encode(text));
  const bits = [];
  const push = (val, n) => { for (let i = n - 1; i >= 0; i--) bits.push((val >> i) & 1); };
  push(4, 4);            // byte 模式
  push(bytes.length, 8); // v1-9 计数 8 位
  for (const b of bytes) push(b, 8);
  const cap = dataCap * 8;
  push(0, Math.min(4, cap - bits.length));   // 终止符
  while (bits.length % 8) bits.push(0);
  const out = [];
  for (let i = 0; i < bits.length; i += 8) {
    let v = 0; for (const b of bits.slice(i, i + 8)) v = (v << 1) | b;
    out.push(v);
  }
  for (let p = 0; out.length < dataCap; p ^= 1) out.push(p ? 0x11 : 0xEC);
  return out;
}

// 主入口：text → 布尔矩阵（true=深色模块）
function qrMatrix(text) {
  const bytes = Array.from(new TextEncoder().encode(text));
  // 版本选择：需求 ≈ 2 码字头 + 文本长度，取首个放得下的版本
  let ver = 0;
  for (let v = 0; v < QR_V.length; v++) { if (bytes.length + 2 <= QR_V[v][0]) { ver = v + 1; break; } }
  if (!ver) throw new Error('QR: text too long (>' + QR_V[4][0] + ' bytes)');
  const [dataCap, ecCap] = QR_V[ver - 1];
  const n = 17 + 4 * ver;

  const M = Array.from({ length: n }, () => new Array(n).fill(false));
  const F = Array.from({ length: n }, () => new Array(n).fill(false)); // 功能模块占位

  const setF = (r, c, v) => { M[r][c] = v; F[r][c] = true; };
  // 定位图案 + 分隔带
  const finder = (r0, c0) => {
    for (let r = -1; r <= 7; r++) for (let c = -1; c <= 7; c++) {
      const rr = r0 + r, cc = c0 + c;
      if (rr < 0 || cc < 0 || rr >= n || cc >= n) continue;
      const dark = r >= 0 && r <= 6 && c >= 0 && c <= 6 && (r === 0 || r === 6 || c === 0 || c === 6 || (r >= 2 && r <= 4 && c >= 2 && c <= 4));
      setF(rr, cc, dark);
    }
  };
  finder(0, 0); finder(0, n - 7); finder(n - 7, 0);
  // 校正图形（仅贯穿两定位图案之间：8..n-9，不得覆盖定位图案本体）
  for (let r = 8; r <= n - 9; r++) setF(r, 6, r % 2 === 0);
  for (let c = 8; c <= n - 9; c++) setF(6, c, c % 2 === 0);
  // 对齐图案（v2+，跳过与定位重叠处）
  const align = QR_ALIGN[ver - 1] || [];
  for (const ar of align) for (const ac of align) {
    if (F[ar][ac]) continue;
    for (let r = -2; r <= 2; r++) for (let c = -2; c <= 2; c++)
      setF(ar + r, ac + c, Math.max(Math.abs(r), Math.abs(c)) !== 1);
  }
  // 暗模块 + 格式信息（ECC L=01，掩码 0）——BCH(15,5) + 0x5412 异或。
  // 位序遵循规范（与 python qrcode 逐位对齐验证）：bit i 从 LSB 起数，
  // 副本一走左上角 L 形、副本二走右下 L 形。
  let fmt = (1 << 3) | 0; // L<<3 | mask
  let rem = fmt << 10;
  for (let i = 14; i >= 10; i--) if ((rem >> i) & 1) rem ^= 0x537 << (i - 10);
  fmt = ((fmt << 10) | rem) ^ 0x5412; // 15 位
  const fb = i => (fmt >> i) & 1;
  // 副本一（左上）：位 0..5 → (i,8)；6 → (7,8)；7 → (8,8)
  for (let i = 0; i <= 5; i++) setF(i, 8, !!fb(i));
  setF(7, 8, !!fb(6)); setF(8, 8, !!fb(7));
  // 副本一续 + 副本二（右下）：位 8..14 → (n-15+i, 8)；位 0..7 → (8, n-1-i)；8 → (8,7)；9..14 → (8,14-i)
  for (let i = 8; i <= 14; i++) setF(n - 15 + i, 8, !!fb(i));
  for (let i = 0; i <= 7; i++) setF(8, n - 1 - i, !!fb(i));
  setF(8, 7, !!fb(8));
  for (let i = 9; i <= 14; i++) setF(8, 14 - i, !!fb(i));
  // 暗模块（恒为深色，位于副本一垂直段末端）
  setF(n - 8, 8, true);


  // 数据码字 + 纠错码字 → 位流
  const dcw = qrDataCodewords(text, dataCap);
  const cw = dcw.concat(rsRem(dcw, ecCap));
  const bits = [];
  for (const b of cw) for (let i = 7; i >= 0; i--) bits.push((b >> i) & 1);

  // 蛇形放置（成对列，从右向左，跳过第 6 列），写数据时直接异或掩码
  let bi = 0, up = true;
  for (let x = n - 1; x > 0; x -= 2) {
    if (x === 6) x--;
    for (let i = 0; i < n; i++) {
      const r = up ? n - 1 - i : i;
      for (const c of [x, x - 1]) {
        if (F[r][c]) continue;
        const bit = bi < bits.length ? bits[bi++] : 0;
        M[r][c] = bit ? !QR_MASK(r, c) : QR_MASK(r, c);
      }
    }
    up = !up;
  }
  return M;
}

// 矩阵 → SVG（quiet zone 4 模块）
function qrSVG(M, px) {
  const n = M.length, q = 4, total = n + q * 2;
  let s = '<svg viewBox="0 0 ' + total + ' ' + total + '" width="' + px + '" height="' + px + '" shape-rendering="crispEdges" role="img" style="background:#fff">';
  for (let r = 0; r < n; r++) for (let c = 0; c < n; c++)
    if (M[r][c]) s += '<rect x="' + (c + q) + '" y="' + (r + q) + '" width="1" height="1"/>';
  return s + '</svg>';
}

/* ── 开学季券码查询（弹窗，仿活动页 #/prizes?tab=vouchers）──────────── */
/* copyText：clipboard API 只在 secure context（https/localhost）可用，
   远程 http 面板会拿不到 navigator.clipboard → 降级 execCommand。 */
function copyText(text) {
  if (navigator.clipboard && window.isSecureContext) return navigator.clipboard.writeText(text);
  return new Promise((resolve, reject) => {
    const ta = document.createElement('textarea');
    ta.value = text;
    ta.style.cssText = 'position:fixed;opacity:0';
    document.body.appendChild(ta);
    ta.select();
    try { document.execCommand('copy') ? resolve() : reject(new Error('copy failed')); }
    catch (e) { reject(e); }
    finally { ta.remove(); }
  });
}

function vcCard(v) {
  const expired = v.valid_to && new Date(v.valid_to) < new Date();
  return '<div class="vc' + (expired ? ' expired' : '') + '">' +
    '<div class="hd"><span class="nm">' + esc(v.prize_name || v.sku_code || '券') + '</span>' +
    (expired ? '<span class="tag bad">已过期</span>' : '<span class="tag ok">可使用</span>') + '</div>' +
    '<div class="meta">' +
      (v.valid_to ? '有效期至 ' + esc(v.valid_to) : '长期有效') +
      (v.granted_at ? ' · ' + esc(v.granted_at.slice(0, 10)) + ' 抽中' : '') +
    '</div>' +
    '<div class="sep"></div>' +
    '<div class="ft"><span class="lab">券码</span><code>' + esc(v.code || '-') + '</code>' +
    '<span class="acts">' +
      (v.code ? '<button class="xs ghost" data-qr="' + esc(v.code) + '">二维码</button>' : '') +
      '<button class="xs ghost" data-copy="' + esc(v.code || '') + '">复制</button>' +
    '</span></div>' +
    '</div>';
}

async function loadSchoolVouchers() {
  const body = $('vcBody');
  $('vcVeil').classList.add('on');
  body.innerHTML = '<div class="state"><span class="dots">查询中</span></div>';
  $('vcNote').textContent = '';
  try {
    const d = await api('school/vouchers');
    const arr = d.accounts || [];
    const ok = arr.filter(a => !a.error);
    const total = ok.reduce((n, a) => n + (a.vouchers || []).length, 0);
    body.innerHTML = ok.filter(a => (a.vouchers || []).length).map(a =>
      '<div class="vc-acct"><span class="nm">' + esc(a.nickname || a.uid) + '</span>' +
      '<span>' + a.vouchers.length + ' 张</span></div>' +
      a.vouchers.map(vcCard).join('')
    ).join('') || '<div class="empty"><div class="big">🎟️</div>还没有抽到券</div>';
    $('vcNote').textContent = total ? total + ' 张券 · ' + ok.filter(a => !(a.vouchers || []).length).length + ' 个账号未抽中' : '';
    const errs = arr.filter(a => a.error);
    if (errs.length) {
      body.insertAdjacentHTML('beforeend', '<div class="note" style="color:var(--warn);margin-top:8px">查询失败：' +
        errs.map(a => esc(a.nickname || a.uid.slice(0, 8)) + '（' + esc(a.error) + '）').join('、') + '</div>');
    }
    body.querySelectorAll('button[data-copy]').forEach(b => b.onclick = async () => {
      try { await copyText(b.dataset.copy); toast('券码已复制', 'ok'); }
      catch (e) { toast('复制失败，请手动选择券码', 'err'); }
    });
    // 二维码：券码本体编码为 QR（到店出示扫描），点击切换显示/隐藏
    body.querySelectorAll('button[data-qr]').forEach(b => b.onclick = () => {
      const card = b.closest('.vc');
      const old = card.querySelector('.vc-qr');
      if (old) { old.remove(); return; }
      const box = document.createElement('div');
      box.className = 'vc-qr';
      try { box.innerHTML = qrSVG(qrMatrix(b.dataset.qr), 148); }
      catch (e) { box.innerHTML = '<span class="note">二维码生成失败：' + esc(e.message) + '</span>'; }
      card.appendChild(box);
    });
  } catch (e) {
    body.innerHTML = '<div class="state err">' + esc(e.message) + '</div>';
  }
}
$('btnSchoolVouchers').onclick = loadSchoolVouchers;
$('btnVcClose').onclick = () => $('vcVeil').classList.remove('on');
$('btnVcRefresh').onclick = loadSchoolVouchers;

/* 成长任务队列。lastQueueSeq 记录本页启动过的队列代次：执行结束后的残留 items
   （running=false 但 seq 停在旧值）不再回写视图——否则扫描结果 3 秒后被上一轮
   队列状态覆盖。 */
let queueTimer = null, lastQueueSeq = 0;
const GROWTH_TITLES = {}; // code → 展示名（扫描时从任务列表带出）
$('btnScanAll').onclick = async () => {
  const b = $('btnScanAll');
  // 停掉队列轮询：显式扫描 = 切到待办视图。否则在途队列的下一 tick 会把扫描
  // 结果冲掉重渲染回队列视图（服务端执行不受影响，只是不再实时回写本视图）。
  if (queueTimer) { clearInterval(queueTimer); queueTimer = null; }
  b.disabled = true; b.textContent = '扫描中…';
  try {
    const d = await api('tasks/scan_all', { method: 'POST' });
    renderQueue(groupItems(d), null, '没有待办任务 🎉', '全部账号的成长任务与开学季活动都已完成，明日再来。');
  } catch (e) { toast(e.message, 'err'); }
  finally { b.disabled = false; b.textContent = '扫描待办'; }
};
$('btnRunQueue').onclick = async () => {
  const conc = Number($('qcConc').value) || 1;
  if (!confirm('扫描全部账号待办并排队执行（账号并发 ' + conc + '，账号内串行）。\n含真实对话的任务耗时较长，确认继续？')) return;
  const b = $('btnRunQueue');
  b.disabled = true; b.textContent = '启动中…';
  try {
    const r = await api('tasks/run_queue', { method: 'POST', body: JSON.stringify({ concurrency: conc }) });
    if (!r.started) { toast(r.message || '没有待办任务', 'ok'); return; }
    lastQueueSeq = r.seq || 0;
    toast('队列已启动：' + r.total + ' 项（并发 ' + conc + '）', 'ok');
    startQueuePolling();
  } catch (e) { toast(e.message, 'err'); }
  finally { b.disabled = false; b.textContent = '执行全部待办'; }
};
// 扫描结果 → 分组条目（无执行状态）
function groupItems(d) {
  const groups = [];
  for (const a of (d.accounts || [])) {
    const rows = [];
    for (const t of (a.growth || [])) {
      GROWTH_TITLES[t.task_code] = t.title || t.task_code;
      rows.push({ kind: 'growth', code: t.task_code, prog: t.target ? t.current + '/' + t.target : '—', status: 'scan' });
    }
    if (rows.length) groups.push({ uid: a.uid, nick: a.nickname, rows });
  }
  return groups;
}
const ST_WORDS = { done: '完成', running: '执行中', error: '失败', skipped: '跳过', pending: '排队', scan: '待执行' };
function qrowHTML(it) {
  const isSchool = it.kind === 'school';
  const title = isSchool ? '开学季闭环' : (GROWTH_TITLES[it.code] || it.code);
  const dotCls = it.status === 'scan' ? 'wait' : it.status === 'running' ? 'run' : it.status === 'error' ? 'err' : it.status === 'skipped' ? 'skip' : it.status === 'done' ? 'done' : 'wait';
  const stWord = it.status === 'scan' ? '待执行' : (ST_WORDS[it.status] || it.status);
  return '<div class="qrow" title="' + esc(it.message || '') + '">' +
    '<span class="code">' + esc(it.code) + '</span>' +
    '<span class="name"><span class="t">' + esc(title) + '</span>' + (isSchool ? '<span class="tag mute">开学季</span>' : '') + '</span>' +
    '<span class="prog">' + esc(it.prog || '') + '</span>' +
    '<span class="st"><span class="qdot ' + dotCls + '"></span>' + stWord + '</span>' +
    '<span class="msg">' + esc(it.message || '') + '</span>' +
    '</div>';
}
function renderQueue(groups, progress, emptyTitle, emptyDesc) {
  const empty = $('tcEmpty'), list = $('qcList');
  if (!groups.length) {
    empty.style.display = '';
    if (emptyTitle) empty.querySelector('.t').textContent = emptyTitle;
    if (emptyDesc) empty.querySelector('.d').textContent = emptyDesc;
    list.innerHTML = '';
    $('qProg').hidden = true; $('qcSummary').textContent = '';
    return;
  }
  empty.style.display = 'none';
  empty.style.display = 'none';
  let total = 0;
  list.innerHTML = groups.map(g => {
    total += g.rows.length;
    return '<div class="qgroup"><header><span class="nm">' + esc(g.nick || g.uid.slice(0, 12)) + '</span><span class="cnt">' + g.rows.length + ' 项待办</span></header>' +
      g.rows.map(qrowHTML).join('') + '</div>';
  }).join('');
  $('qcSummary').textContent = total + ' 项';
  updateProgress(progress);
}
function updateProgress(q) {
  if (!q || !q.items) { $('qProg').hidden = true; return; }
  const total = q.items.length;
  const done = q.items.filter(it => it.status === 'done' || it.status === 'error' || it.status === 'skipped').length;
  $('qProg').hidden = false;
  $('qBarFill').style.width = (total ? Math.round(done / total * 100) : 0) + '%';
  $('qProgText').textContent = (q.running ? '执行中 ' : '已结束 ') + done + ' / ' + total;
}
// 队列状态 → 分组（执行时轮询）
function groupsFromQueue(items) {
  const by = new Map();
  for (const it of items) {
    if (!by.has(it.uid)) by.set(it.uid, { uid: it.uid, nick: it.nickname, rows: [] });
    by.get(it.uid).rows.push({
      kind: it.kind, code: it.code,
      prog: it.kind === 'school' ? '—' : '',
      status: it.status, message: it.message,
    });
  }
  return Array.from(by.values());
}
function startQueuePolling() {
  if (queueTimer) clearInterval(queueTimer);
  queueTimer = setInterval(async () => {
    let q;
    try { q = await api('tasks/queue'); } catch (e) { return; }
    if (!q.started) return;
    // 只渲染本页启动过的那轮队列（刷新页面后不再接管旧队列）。
    if (lastQueueSeq && q.seq !== lastQueueSeq) return;
    if (q.running) {
      renderQueue(groupsFromQueue(q.items || []), q);
      return;
    }
    // 结束：终态只渲染这一次，随即停表。此后残留的 items（running=false）不再
    // 回写视图——曾把用户刚点开的「扫描待办」结果在下一个 tick 冲掉。
    renderQueue(groupsFromQueue(q.items || []), q);
    clearInterval(queueTimer); queueTimer = null;
    toast('任务队列执行结束', 'ok');
  }, 3000);
}
// reattachQueueView 切回任务中心视图时恢复队列进度：仅当本页启动的队列仍在
// 执行才重新开轮询（残留态/别页队列不接管——视图不被旧结果冲掉）。
function reattachQueueView() {
  // 全程异步：go() 在顶层（app.js ~143 行）被调用时，本文件下方 let/const
  //（queueTimer/lastQueueSeq 等）尚未初始化——同步读取即 TDZ ReferenceError
  // 使整个脚本中断。await 之后才碰它们（旧 pollQueueOnce 正是靠开头的 await
  // 侥幸安全）。queueTimer 的"已在跑"判定也挪到 await 后，语义不变。
  (async () => {
    try {
      const q = await api('tasks/queue');
      if (queueTimer) return; // 轮询已在跑（跨视图不中断）
      if (q.started && q.running && (!lastQueueSeq || q.seq === lastQueueSeq)) startQueuePolling();
    } catch (e) { /* 静默 */ }
  })();
}

/* ── 用量 ─────────────────────────────────────────────────────────── */
/* 图表用原生 SVG 手绘：面板是 go:embed 单文件、无构建步骤，引入图表库
   就得带上打包器，得不偿失。这里只需要堆叠柱状图，二十行足够。 */

function fmtTok(n) {
  n = Number(n || 0);
  if (n >= 1e9) return (n / 1e9).toFixed(2) + 'B';
  if (n >= 1e6) return (n / 1e6).toFixed(2) + 'M';
  if (n >= 1e3) return (n / 1e3).toFixed(1) + 'k';
  return String(Math.round(n));
}
function fmtMs(ms) {
  ms = Number(ms || 0);
  if (!ms) return '—';
  if (ms >= 1000) return (ms / 1000).toFixed(2) + 's';
  return Math.round(ms) + 'ms';
}
function fmtRate(r) { return r ? Number(r).toFixed(1) + ' tok/s' : '—'; }

function usStat(v, k, cls) {
  return '<div class="stat ' + (cls || '') + '"><div class="v">' + esc(v) +
         '</div><div class="k">' + esc(k) + '</div></div>';
}

function usBar(prompt, completion, total) {
  const t = Number(total || 0);
  if (!t) return '';
  const pp = Math.max(0, Math.min(100, Number(prompt || 0) / t * 100));
  const pc = Math.max(0, Math.min(100, Number(completion || 0) / t * 100));
  return '<span class="us-wrapbar">' +
    '<span class="bar bar-p" style="width:' + (pp * 0.8).toFixed(1) + 'px" title="prompt"></span>' +
    '<span class="bar bar-c" style="width:' + Math.max(2, pc * 0.8).toFixed(1) + 'px" title="completion"></span>' +
    '</span>';
}

/* usRow 生成一行。mid 是插在「名称」之后、请求数之前的额外单元格（如「域」列）。
   withPerf 控制是否追加延迟/速率两列——只有「按账号」表的表头带这两列；
   模型表与域表没有，多输出会造成列错位。早先靠「mid 是否为 undefined」隐式
   判断，调用方稍一改动就会错列，故改为显式参数。 */
function usRow(name, sub, a, mid, withPerf) {
  return '<tr>' +
    '<td class="mark" aria-hidden="true"></td>' +
    '<td>' + esc(name) + (sub ? '<div class="note">' + esc(sub) + '</div>' : '') + '</td>' +
    (mid || '') +
    '<td class="num">' + fmtTok(a.requests) + '</td>' +
    '<td class="num">' + (a.errors ? '<span style="color:var(--warn)">' + fmtTok(a.errors) + '</span>' : '—') + '</td>' +
    '<td class="num">' + fmtTok(a.prompt_tokens) + '</td>' +
    '<td class="num">' + fmtTok(a.completion_tokens) + '</td>' +
    '<td class="num">' + fmtTok(a.total_tokens) + '</td>' +
    (withPerf
      ? '<td class="num">' + fmtMs(a.avg_latency_ms) + '</td>' +
        '<td class="num">' + fmtRate(a.avg_tokens_per_second) + '</td>'
      : '') +
    '</tr>';
}

let usageDataCache = null;

function renderUsage(d) {
  usageDataCache = d;
  const t = d.totals || {};
  const cachedTokens = t.cached_tokens || 0;
  const promptTokens = t.prompt_tokens || 0;
  const hitRate = promptTokens > 0 ? (cachedTokens / promptTokens * 100) : 0;
  const savedCredits = cachedTokens * 0.9 / 1000 * 1.0;
  const totalTokens = t.total_tokens || 0;
  const requests = t.requests || 0;

  // 1. DeepSeek 风格 4 核心 KPI 卡片
  const kpiEl = $('dsKpiGrid');
  if (kpiEl) {
    // 腾讯 WorkBuddy 官方价格基准：加量包 50 元 / 1000 积分 = 0.05 元 / 积分
    const OFFICIAL_CREDIT_PRICE = 0.05;
    const OFFICIAL_MODEL_RATES = {
      'kimi-k3-1': 1.62,
      'glm-5.3': 0.79,
      'deepseek-v4-pro': 0.51,
      'deepseek-v4-flash': 0.11,
      'glm-5.3-flash': 0.06,
      'hy3': 0.0,
    };

    let estCredits = 0;
    let savedCredits = 0;
    if (Array.isArray(d.by_model) && d.by_model.length > 0) {
      for (const m of d.by_model) {
        const rate = OFFICIAL_MODEL_RATES[m.key] !== undefined ? OFFICIAL_MODEL_RATES[m.key] : 1.0;
        const tt = Number(m.total_tokens || 0);
        const ct = Number(m.cached_tokens || 0);
        estCredits += (tt / 1000) * rate;
        savedCredits += (ct * 0.9 / 1000) * rate;
      }
    } else {
      estCredits = (totalTokens / 1000) * 1.0;
      savedCredits = (cachedTokens * 0.9 / 1000) * 1.0;
    }

    const estCny = estCredits * OFFICIAL_CREDIT_PRICE;
    const savedCny = savedCredits * OFFICIAL_CREDIT_PRICE;
    const cnyStr = estCny >= 10 ? estCny.toFixed(2) : estCny.toFixed(3);
    const savedCnyStr = savedCny >= 10 ? savedCny.toFixed(2) : savedCny.toFixed(3);

    kpiEl.innerHTML = `
      <div class="ds-kpi-card">
        <div class="lbl"><span>预估消耗额度</span><span class="tag ok" style="font-size:10px;">官方 ¥0.05/积分</span></div>
        <div class="val">¥ ${cnyStr}</div>
        <div class="sub">折合约 ${estCredits.toFixed(1)} 积分 · 节省 ~¥${savedCnyStr} (${savedCredits.toFixed(1)} 积分)</div>
      </div>
      <div class="ds-kpi-card">
        <div class="lbl"><span>API 请求次数</span><span style="font-size:11px;color:var(--ink-3);">含重试</span></div>
        <div class="val">${fmtTok(requests)}</div>
        <div class="sub">成功率: ${requests > 0 ? (((requests - (t.errors || 0)) / requests) * 100).toFixed(1) : 100}% · 失败尝试 ${t.errors || 0}</div>
      </div>
      <div class="ds-kpi-card">
        <div class="lbl"><span>Tokens 消耗总计</span></div>
        <div class="val">${fmtTok(totalTokens)}</div>
        <div class="sub">Prompt: ${fmtTok(promptTokens)} / Output: ${fmtTok(t.completion_tokens || 0)}</div>
      </div>
      <div class="ds-kpi-card">
        <div class="lbl"><span>Prompt Cache 命中率</span><span class="tag ${hitRate > 50 ? 'ok' : (hitRate > 0 ? 'warn' : 'mute')}" style="font-size:10px;">前缀复用</span></div>
        <div class="val" style="color:${hitRate > 50 ? 'var(--ok)' : (hitRate > 0 ? 'var(--warn)' : 'inherit')};">${hitRate.toFixed(1)}%</div>
        <div class="sub">已命中缓存 Token: ${fmtTok(cachedTokens)}</div>
      </div>
    `;
  }

  // 顶部标题数字
  if ($('chartReqTotal')) $('chartReqTotal').textContent = fmtTok(requests);
  if ($('chartTokTotal')) $('chartTokTotal').textContent = fmtTok(totalTokens);

  // 填充 API 令牌筛选下拉
  const tokenSel = $('usTokenFilter');
  if (tokenSel) {
    const curVal = tokenSel.value;
    const tokens = d.by_token || [];
    tokenSel.innerHTML = '<option value="">全部令牌</option>' +
      tokens.map(tk => `<option value="${esc(tk.key)}"${tk.key === curVal ? ' selected' : ''}>${esc(tk.key)} (${fmtTok(tk.requests)}次)</option>`).join('');
    tokenSel.onchange = () => filterAndRenderCharts();
  }

  // 元信息
  const winLabel = ($('usWindow') && $('usWindow').selectedOptions[0]) ?
    $('usWindow').selectedOptions[0].textContent.trim() : '';
  if ($('usNote')) {
    $('usNote').textContent =
      (winLabel ? winLabel + ' · ' : '') +
      (d.buckets || 0) + ' 个分桶' +
      (d.since ? ' · 自 ' + d.since.replace('T', ' ') : '');
  }

  // 渲染下钻表格
  if ($('usTokenBody')) {
    $('usTokenBody').innerHTML = (d.by_token || []).map(x => {
      const pt = x.prompt_tokens || 0;
      const ct = x.cached_tokens || 0;
      const hr = pt > 0 ? (ct / pt * 100) : 0;
      const badge = hr > 50 ? 'ok' : (hr > 20 ? 'warn' : 'mute');
      const hrHtml = '<span class="tag ' + badge + '">' + hr.toFixed(1) + '%</span>';
      const sc = ct * 0.9 / 1000 * 1.0;
      return '<tr>' +
        '<td class="mark" aria-hidden="true"></td>' +
        '<td><b>' + esc(x.key) + '</b>' + (x.extra ? '<div class="note">' + esc(x.extra) + '</div>' : '') + '</td>' +
        '<td class="num">' + fmtTok(x.requests) + '</td>' +
        '<td class="num">' + (x.errors ? '<span style="color:var(--warn)">' + fmtTok(x.errors) + '</span>' : '—') + '</td>' +
        '<td class="num">' + fmtTok(pt) + '</td>' +
        '<td class="num" style="color:var(--accent)">' + fmtTok(ct) + '</td>' +
        '<td>' + hrHtml + '</td>' +
        '<td class="num">' + fmtTok(x.completion_tokens) + '</td>' +
        '<td class="num"><b>' + fmtTok(x.total_tokens) + '</b></td>' +
        '<td class="num" style="color:var(--ok)">~' + sc.toFixed(1) + '</td>' +
        '</tr>';
    }).join('') || '<tr><td colspan="10" class="empty">暂无数据</td></tr>';
  }

  if ($('usAccBody')) {
    $('usAccBody').innerHTML = (d.by_account || []).map(x =>
      usRow(x.key.slice(0, 8), x.extra || '', x,
        '<td class="num">' + esc(x.realm || '') + '</td>', true)
    ).join('') || '<tr><td colspan="10" class="empty">暂无数据</td></tr>';
  }

  if ($('usModelBody')) {
    $('usModelBody').innerHTML = (d.by_model || []).map(x =>
      usRow(x.key, '', x, '', false)).join('') || '<tr><td colspan="7" class="empty">暂无数据</td></tr>';
  }

  if ($('usRealmBody')) {
    $('usRealmBody').innerHTML = (d.by_realm || []).map(x =>
      usRow(x.key, '', x, '', false)).join('') || '<tr><td colspan="7" class="empty">暂无数据</td></tr>';
  }

  filterAndRenderCharts();
}

let currentTokChartMode = 'type'; // 'type' | 'model'

function setTokChartMode(mode) {
  if (currentTokChartMode === mode) return;
  currentTokChartMode = mode;
  if ($('tokModeType')) $('tokModeType').classList.toggle('active', mode === 'type');
  if ($('tokModeModel')) $('tokModeModel').classList.toggle('active', mode === 'model');
  filterAndRenderCharts();
}

// 统一获取所选时间窗口小时数，支持 'month' (本月至今)、'0' (全部历史) 及固定小时
function getUsWindowHours() {
  const sel = $('usWindow');
  const val = sel ? sel.value : '720';
  if (val === 'month') {
    const now = new Date();
    const startOfMonth = new Date(now.getFullYear(), now.getMonth(), 1, 0, 0, 0, 0);
    const diffHours = Math.ceil((now.getTime() - startOfMonth.getTime()) / 3600000);
    return Math.max(1, diffHours);
  }
  if (val === '0' || val === 0) return 0;
  const n = Number(val);
  return Number.isFinite(n) ? n : 720;
}

function filterAndRenderCharts() {
  if (!usageDataCache) return;
  const hours = getUsWindowHours();
  const series = usageDataCache.series || [];
  renderReqAreaChart(series, hours);
  renderTokBarChart(series, hours);
}

/* parsePointTime 把后端的 t 解析成毫秒时间戳。 */
function parsePointTime(p) {
  const s = p.t.length === 13 ? p.t + ':00:00' : p.t + 'T00:00:00';
  const d = new Date(s);
  return isNaN(d.getTime()) ? null : d.getTime();
}

/* 核心修复：基于选定窗口计算真实时间轴跨度，避免单点挤在最左边 */
function getTimeWindowSpan(pts, hours) {
  const now = Date.now();
  const curWinVal = ($('usWindow') && $('usWindow').value) || '';
  if (curWinVal === 'month') {
    const dNow = new Date(now);
    const startOfMonth = new Date(dNow.getFullYear(), dNow.getMonth(), 1, 0, 0, 0, 0);
    const t0 = startOfMonth.getTime();
    return { t0, t1: now, span: Math.max(3600000, now - t0) };
  }
  if (hours > 0) {
    const tStart = now - hours * 3600 * 1000;
    return { t0: tStart, t1: now, span: hours * 3600 * 1000 };
  }
  if (!pts.length) {
    return { t0: now - 86400000, t1: now, span: 86400000 };
  }
  let tMin = pts[0].t, tMax = pts[pts.length - 1].t;
  for (const p of pts) {
    if (p.t < tMin) tMin = p.t;
    if (p.t > tMax) tMax = p.t;
  }
  if (tMin === tMax) {
    tMin -= 3600 * 1000 * 12;
    tMax += 3600 * 1000 * 12;
  }
  return { t0: tMin, t1: tMax, span: Math.max(3600000, tMax - tMin) };
}

/* 1. DeepSeek 风格：API 请求次数面积折线图 */
function renderReqAreaChart(series, hours) {
  const host = $('chartReqArea');
  if (!host) return;

  const pts = [];
  for (const p of series) {
    const t = parsePointTime(p);
    if (t !== null) pts.push({ t, req: Number(p.requests || 0), raw: p.t, scope: p.scope });
  }

  if (!pts.length) {
    host.innerHTML = '<div class="us-empty">暂无请求数据</div>';
    return;
  }

  const W = 460, H = 160, PL = 54, PR = 16, PT = 15, PB = 28;
  const iw = W - PL - PR, ih = H - PT - PB;
  const { t0, span } = getTimeWindowSpan(pts, hours);

  const maxVal = Math.max(4, ...pts.map(p => p.req));
  const xOf = t => PL + Math.max(0, Math.min(iw, (t - t0) / span * iw));
  const yOf = v => PT + ih - (v / maxVal * ih);

  let out = '<svg viewBox="0 0 ' + W + ' ' + H + '" role="img" preserveAspectRatio="none">';
  out += `<defs>
    <linearGradient id="reqGrad" x1="0" y1="0" x2="0" y2="1">
      <stop offset="0%" stop-color="#3b82f6" stop-opacity="0.32"/>
      <stop offset="100%" stop-color="#3b82f6" stop-opacity="0.01"/>
    </linearGradient>
  </defs>`;

  // y 轴 3 条干净的横向网格线 + 文字（绝对不和图形重叠，取整格式化）
  for (let i = 0; i <= 3; i++) {
    const y = PT + ih - (ih * i / 3);
    const tickVal = Math.round(maxVal * i / 3);
    out += '<line class="gl" x1="' + PL + '" y1="' + y.toFixed(1) + '" x2="' + (W - PR) + '" y2="' + y.toFixed(1) + '"/>';
    out += '<text class="tk" x="' + (PL - 8) + '" y="' + (y + 3.5).toFixed(1) + '" text-anchor="end">' + fmtTok(tickVal) + '</text>';
  }

  // 坐标点计算
  const coords = pts.map(p => ({ x: xOf(p.t), y: yOf(p.req), p }));
  // 首尾补全（若窗口较大）
  let pathD = '';
  if (coords.length === 1) {
    const c = coords[0];
    pathD = `M ${PL} ${PT+ih} L ${c.x - 20} ${PT+ih} L ${c.x} ${c.y} L ${c.x + 20} ${PT+ih} L ${W-PR} ${PT+ih}`;
  } else {
    pathD = `M ${coords[0].x.toFixed(1)} ${coords[0].y.toFixed(1)}`;
    for (let i = 1; i < coords.length; i++) {
      const prev = coords[i - 1];
      const cur = coords[i];
      const mx = (prev.x + cur.x) / 2;
      pathD += ` C ${mx.toFixed(1)} ${prev.y.toFixed(1)}, ${mx.toFixed(1)} ${cur.y.toFixed(1)}, ${cur.x.toFixed(1)} ${cur.y.toFixed(1)}`;
    }
  }

  // 面积填充
  const areaD = pathD + ` L ${coords[coords.length - 1].x.toFixed(1)} ${(PT + ih).toFixed(1)} L ${coords[0].x.toFixed(1)} ${(PT + ih).toFixed(1)} Z`;
  out += '<path d="' + areaD + '" fill="url(#reqGrad)"/>';
  // 曲线轮廓
  out += '<path d="' + pathD + '" fill="none" stroke="#2563eb" stroke-width="2" stroke-linecap="round"/>';

  // 关键数据节点小圆圈
  for (const c of coords) {
    out += `<circle cx="${c.x.toFixed(1)}" cy="${c.y.toFixed(1)}" r="3" fill="#ffffff" stroke="#2563eb" stroke-width="2">` +
      `<title>${esc(c.p.raw)}: ${c.p.req} 次请求</title></circle>`;
  }

  // X 轴基线
  out += '<line class="ax" x1="' + PL + '" y1="' + (PT + ih) + '" x2="' + (W - PR) + '" y2="' + (PT + ih) + '"/>';

  // X 轴日期刻度（等间距 4 个标尺，首尾不贴边）
  for (let k = 0; k <= 3; k++) {
    const t = t0 + span * (k / 3);
    const d = new Date(t);
    const lab = (hours <= 24)
      ? String(d.getHours()).padStart(2, '0') + ':00'
      : (d.getMonth() + 1) + '/' + d.getDate();
    const cx = PL + (k / 3) * iw;
    out += '<text class="tk" x="' + cx.toFixed(1) + '" y="' + (PT + ih + 15) + '" text-anchor="middle">' + esc(lab) + '</text>';
  }

  out += '</svg>';
  host.innerHTML = out;
}

/* 2. DeepSeek 风格：Tokens 消耗与缓存堆叠柱状图（支持类型/模型双维度 + 日历补零 + 防重叠） */
function renderTokBarChart(series, hours) {
  const host = $('chartTokBar');
  if (!host) return;

  const rawPts = [];
  for (const p of series) {
    const t = parsePointTime(p);
    if (t === null) continue;
    const pt = Number(p.prompt_tokens || 0);
    const ct = Number(p.completion_tokens || 0);
    const cached = Number(p.cached_tokens || 0);
    const tt = Number(p.total_tokens || 0) || (pt + ct);
    rawPts.push({
      t,
      scope: p.scope,
      raw: p.t,
      pt,
      ct,
      cached,
      tt,
      models: p.models || null,
      req: Number(p.requests || 0),
    });
  }

  if (!rawPts.length) {
    host.innerHTML = '<div class="us-empty">暂无 Token 消耗数据</div>';
    if ($('chartTokLegend')) $('chartTokLegend').innerHTML = '';
    return;
  }

  const W = 460, H = 160, PL = 54, PR = 16, PT = 15, PB = 28;
  const iw = W - PL - PR, ih = H - PT - PB;
  const { t0, t1, span } = getTimeWindowSpan(rawPts, hours);

  // 1. 确定粒度：hours <= 48 为小时槽；hours > 48 (及全历史) 为日槽
  const isHourly = (hours > 0 && hours <= 48);
  const pad = n => String(n).padStart(2, '0');
  const slots = [];

  if (isHourly) {
    const slotCount = Math.max(1, hours > 0 ? hours : 24);
    const d1 = new Date(t1);
    const endHour = new Date(d1.getFullYear(), d1.getMonth(), d1.getDate(), d1.getHours(), 0, 0, 0);
    for (let i = 0; i < slotCount; i++) {
      const cur = new Date(endHour.getTime() - (slotCount - 1 - i) * 3600 * 1000);
      const key = `${cur.getFullYear()}-${pad(cur.getMonth() + 1)}-${pad(cur.getDate())}T${pad(cur.getHours())}`;
      slots.push({
        key,
        raw: key,
        t: cur.getTime(),
        pt: 0,
        ct: 0,
        cached: 0,
        tt: 0,
        models: {},
        req: 0,
      });
    }
  } else {
    const d0 = new Date(t0);
    const d1 = new Date(t1);
    const startDate = new Date(d0.getFullYear(), d0.getMonth(), d0.getDate());
    const endDate = new Date(d1.getFullYear(), d1.getMonth(), d1.getDate());
    let cur = new Date(startDate.getTime());
    while (cur.getTime() <= endDate.getTime()) {
      const key = `${cur.getFullYear()}-${pad(cur.getMonth() + 1)}-${pad(cur.getDate())}`;
      slots.push({
        key,
        raw: key,
        t: cur.getTime(),
        pt: 0,
        ct: 0,
        cached: 0,
        tt: 0,
        models: {},
        req: 0,
      });
      cur.setDate(cur.getDate() + 1);
    }
    if (!slots.length) {
      const key = `${endDate.getFullYear()}-${pad(endDate.getMonth() + 1)}-${pad(endDate.getDate())}`;
      slots.push({
        key,
        raw: key,
        t: endDate.getTime(),
        pt: 0,
        ct: 0,
        cached: 0,
        tt: 0,
        models: {},
        req: 0,
      });
    }
  }

  // 2. 将 series 聚合填充入对应日历槽（实现日历补零与数据汇总）
  const slotMap = new Map();
  for (const s of slots) {
    slotMap.set(s.key, s);
  }

  for (const p of rawPts) {
    const key = isHourly ? (p.raw.length === 13 ? p.raw : p.raw + 'T00') : p.raw.slice(0, 10);
    const slot = slotMap.get(key);
    if (!slot) continue;

    slot.pt += p.pt;
    slot.ct += p.ct;
    slot.cached += p.cached;
    slot.tt += p.tt;
    slot.req += p.req;

    if (p.models && typeof p.models === 'object') {
      for (const [mName, mTokens] of Object.entries(p.models)) {
        const val = Number(mTokens || 0);
        if (val > 0) {
          slot.models[mName] = (slot.models[mName] || 0) + val;
        }
      }
    }
  }

  const maxVal = Math.max(10, ...slots.map(s => s.tt));
  let out = '<svg viewBox="0 0 ' + W + ' ' + H + '" role="img" preserveAspectRatio="none">';

  // 3. y 轴 3 条横向刻度线 + 文字（保持 8px 以上安全 Padding，数值四舍五入）
  for (let i = 0; i <= 3; i++) {
    const y = PT + ih - (ih * i / 3);
    const tickVal = Math.round(maxVal * i / 3);
    out += '<line class="gl" x1="' + PL + '" y1="' + y.toFixed(1) + '" x2="' + (W - PR) + '" y2="' + y.toFixed(1) + '"/>';
    out += '<text class="tk" x="' + (PL - 8) + '" y="' + (y + 3.5).toFixed(1) + '" text-anchor="end">' + fmtTok(tickVal) + '</text>';
  }

  // 4. 严密的防重叠栅格算法 (Density Guard)
  const slotWidth = iw / slots.length;
  const minGap = Math.max(2, Math.min(6, slotWidth * 0.25));
  const bw = Math.max(3, Math.min(24, slotWidth - minGap));

  // 5. 根据模式分支渲染
  if (currentTokChartMode === 'model') {
    // ── 按模型模式 ──
    const modelTotals = {};
    for (const s of slots) {
      for (const [mName, mTok] of Object.entries(s.models || {})) {
        const val = Number(mTok || 0);
        if (val > 0) modelTotals[mName] = (modelTotals[mName] || 0) + val;
      }
    }
    const sortedModels = Object.keys(modelTotals).sort((a, b) => modelTotals[b] - modelTotals[a]);
    const top4 = sortedModels.slice(0, 4);

    const MODEL_COLORS = ['#3b82f6', '#10b981', '#f59e0b', '#8b5cf6'];
    const COLOR_OTHER = '#94a3b8';
    const modelColorMap = {};
    top4.forEach((m, idx) => {
      modelColorMap[m] = MODEL_COLORS[idx % MODEL_COLORS.length];
    });

    const hasOther = sortedModels.length > 4 || slots.some(s => {
      const top4Tok = top4.reduce((sum, m) => sum + (s.models[m] || 0), 0);
      return s.tt > top4Tok;
    });

    // 动态更新图例
    if ($('chartTokLegend')) {
      let legendHtml = '';
      top4.forEach(m => {
        legendHtml += `<span><i class="sw" style="background:${modelColorMap[m]};"></i>${esc(m)}</span>`;
      });
      if (hasOther) {
        legendHtml += `<span><i class="sw" style="background:${COLOR_OTHER};"></i>其他</span>`;
      }
      if (!top4.length && !hasOther) {
        legendHtml = `<span><i class="sw" style="background:${COLOR_OTHER};"></i>暂无模型数据</span>`;
      }
      $('chartTokLegend').innerHTML = legendHtml;
    }

    // 渲染各槽模型堆叠柱
    for (let i = 0; i < slots.length; i++) {
      const s = slots[i];
      if (s.tt <= 0) continue;

      const x = PL + (i + 0.5) * slotWidth - bw / 2;
      const hTot = ih * (s.tt / maxVal);

      const slotBuckets = [];
      let top4Sum = 0;
      top4.forEach(m => {
        const tok = Number(s.models[m] || 0);
        if (tok > 0) {
          slotBuckets.push({ name: m, tokens: tok, color: modelColorMap[m] });
          top4Sum += tok;
        }
      });

      let otherTokens = 0;
      for (const [mName, mTok] of Object.entries(s.models || {})) {
        if (!top4.includes(mName)) {
          otherTokens += Number(mTok || 0);
        }
      }
      if (s.tt > top4Sum + otherTokens) {
        otherTokens += (s.tt - (top4Sum + otherTokens));
      }
      if (otherTokens > 0) {
        slotBuckets.push({ name: '其他', tokens: otherTokens, color: COLOR_OTHER });
      }
      if (slotBuckets.length === 0) {
        slotBuckets.push({ name: '其他', tokens: s.tt, color: COLOR_OTHER });
      }

      // 计算各模型分块高度，严格保证高度和等于 hTot
      let allocatedH = 0;
      for (let b = 0; b < slotBuckets.length; b++) {
        if (b === slotBuckets.length - 1) {
          slotBuckets[b].h = Math.max(0, hTot - allocatedH);
        } else {
          slotBuckets[b].h = hTot * (slotBuckets[b].tokens / s.tt);
          allocatedH += slotBuckets[b].h;
        }
      }

      const visible = slotBuckets.filter(b => b.h > 0.05);
      if (visible.length === 0 && hTot > 0.05) {
        visible.push({ name: '其他', tokens: s.tt, color: COLOR_OTHER, h: hTot });
      }
      if (visible.length > 0) {
        let vAlloc = 0;
        for (let b = 0; b < visible.length - 1; b++) vAlloc += visible[b].h;
        visible[visible.length - 1].h = Math.max(0, hTot - vAlloc);
      }

      let currY = PT + ih;
      let barSvg = '';
      for (let b = 0; b < visible.length; b++) {
        const bucket = visible[b];
        const isTop = (b === visible.length - 1);
        const rxAttr = isTop ? ' rx="2"' : '';
        currY -= bucket.h;
        barSvg += `<rect x="${x.toFixed(1)}" y="${currY.toFixed(1)}" width="${bw.toFixed(1)}" height="${bucket.h.toFixed(1)}" fill="${bucket.color}"${rxAttr}/>`;
      }

      let tip = `${esc(s.raw)}\n`;
      for (const b of slotBuckets) {
        tip += `${esc(b.name)}: ${fmtTok(b.tokens)}\n`;
      }
      tip += `合计: ${fmtTok(s.tt)} Tokens`;
      out += `<g>${barSvg}<title>${tip}</title></g>`;
    }
  } else {
    // ── 按类型模式 ──
    if ($('chartTokLegend')) {
      $('chartTokLegend').innerHTML =
        '<span><i class="sw" style="background:#2563eb;"></i>Prompt</span>' +
        '<span><i class="sw" style="background:#60a5fa;"></i>Cache命中</span>' +
        '<span><i class="sw" style="background:#10b981;"></i>Completion</span>';
    }

    // 渲染各槽类型堆叠柱：Prompt(#2563eb) -> Cache(#60a5fa) -> Completion(#10b981)
    for (let i = 0; i < slots.length; i++) {
      const s = slots[i];
      if (s.tt <= 0) continue;

      const x = PL + (i + 0.5) * slotWidth - bw / 2;
      const hTot = ih * (s.tt / maxVal);

      const realPrompt = Math.max(0, s.pt - s.cached);
      const cached = Math.min(s.cached, s.pt);
      const comp = s.ct;
      const tokSum = realPrompt + cached + comp;

      let hRealPrompt = 0, hCached = 0, hComp = 0;
      if (tokSum > 0) {
        hRealPrompt = hTot * (realPrompt / tokSum);
        hCached = hTot * (cached / tokSum);
        hComp = Math.max(0, hTot - hRealPrompt - hCached);
      } else {
        hRealPrompt = hTot;
      }

      const segments = [];
      if (hRealPrompt > 0.05) segments.push({ h: hRealPrompt, fill: '#2563eb' });
      if (hCached > 0.05) segments.push({ h: hCached, fill: '#60a5fa' });
      if (hComp > 0.05) segments.push({ h: hComp, fill: '#10b981' });
      if (segments.length === 0 && hTot > 0.05) segments.push({ h: hTot, fill: '#2563eb' });

      if (segments.length > 0) {
        let accH = 0;
        for (let b = 0; b < segments.length - 1; b++) accH += segments[b].h;
        segments[segments.length - 1].h = Math.max(0, hTot - accH);
      }

      let currY = PT + ih;
      let barSvg = '';
      for (let b = 0; b < segments.length; b++) {
        const seg = segments[b];
        const isTop = (b === segments.length - 1);
        const rxAttr = isTop ? ' rx="2"' : '';
        currY -= seg.h;
        barSvg += `<rect x="${x.toFixed(1)}" y="${currY.toFixed(1)}" width="${bw.toFixed(1)}" height="${seg.h.toFixed(1)}" fill="${seg.fill}"${rxAttr}/>`;
      }

      const tip = `${esc(s.raw)}\nPrompt: ${fmtTok(s.pt)} (Cache: ${fmtTok(s.cached)})\nCompletion: ${fmtTok(s.ct)}\n合计: ${fmtTok(s.tt)} Tokens`;
      out += `<g>${barSvg}<title>${tip}</title></g>`;
    }
  }

  // 6. X 轴基线
  out += '<line class="ax" x1="' + PL + '" y1="' + (PT + ih) + '" x2="' + (W - PR) + '" y2="' + (PT + ih) + '"/>';

  // 7. X 轴日期刻度（与 renderReqAreaChart 完全同步）
  for (let k = 0; k <= 3; k++) {
    const t = t0 + span * (k / 3);
    const d = new Date(t);
    const lab = (hours <= 24)
      ? String(d.getHours()).padStart(2, '0') + ':00'
      : (d.getMonth() + 1) + '/' + d.getDate();
    const cx = PL + (k / 3) * iw;
    out += '<text class="tk" x="' + cx.toFixed(1) + '" y="' + (PT + ih + 15) + '" text-anchor="middle">' + esc(lab) + '</text>';
  }

  out += '</svg>';
  host.innerHTML = out;
}

async function loadUsage() {
  const hours = getUsWindowHours();
  try {
    const d = await api('usage?hours=' + encodeURIComponent(hours));
    renderUsage(d);
  } catch (e) {
    if ($('chartTokBar')) $('chartTokBar').innerHTML = '<div class="us-empty">读取用量失败：' + esc(e.message) + '</div>';
    if ($('chartReqArea')) $('chartReqArea').innerHTML = '<div class="us-empty">读取用量失败：' + esc(e.message) + '</div>';
  }
}

if ($('btnUsage')) $('btnUsage').onclick = loadUsage;
if ($('usWindow')) $('usWindow').onchange = loadUsage;
if ($('tokModeType')) $('tokModeType').onclick = () => setTokChartMode('type');
if ($('tokModeModel')) $('tokModeModel').onclick = () => setTokChartMode('model');

/* ── 积分构成 ─────────────────────────────────────────────────────── */
/* 一个账号的余额是若干积分包之和。包按来源命名（「国内运营裂变包」「拉新权益包」
   「个人体验版」…），面额从 6 到 1500 不等，且**按次发放**。所以两个任务完成度
   完全一致的账号，余额可能差上千——差别只在包里。这里把逐包明细摊开，并给每个
   包名一个稳定配色，跨账号对比时同色即同类。 */

const PK_COLORS = ['#4f8cff', '#25b08b', '#e8a33d', '#c96bd6', '#e2607a',
                   '#5aa9e6', '#8fbf3f', '#b58b5a', '#7d8fa8', '#d4785c'];
const PK_ACCOUNT_COLORS = ['#4f8cff', '#25b08b', '#e8a33d', '#c96bd6',
                           '#e2607a', '#20a4a4', '#8fbf3f', '#d4785c',
                           '#7c83db', '#c48a2f', '#b45f8c', '#5aa9e6'];

function pkColor(i) { return PK_COLORS[i % PK_COLORS.length]; }

// pkAccountColorMap 按 UID 稳定分配颜色：排序后分配，账号刷新/重排不会换色。
function pkAccountColorMap(list) {
  const uids = (list || [])
    .filter(a => a && !a.error && a.uid)
    .map(a => String(a.uid))
    .sort();
  const colors = new Map();
  uids.forEach((uid, i) => colors.set(uid, PK_ACCOUNT_COLORS[i % PK_ACCOUNT_COLORS.length]));
  return colors;
}

/* pkBySource 把包按名称归并，得到「来源 → 面额/余额/个数」。这是对比的关键视图：
   两个号的差异一定体现在某几个来源的面额上。 */
function pkBySource(packs) {
  const m = new Map();
  for (const p of packs) {
    // 分组键用 code + name，而不是只 name：上游给「首登赠送」和普通活动包用了
    // **同一个 PackageName 和同一个 PackageCode**，只按 name 会把两类混成一类，
    // 那正是当初「两个号为何差 1500」看不出来的原因。这里至少把 code 带进键里，
    // 并在卡片上显示最早的发放时间。
    const k = (p.package_code || '') + '|' + (p.name || '(未命名)');
    const e = m.get(k) || {
      key: k, name: p.name || '(未命名)', code: p.package_code || '',
      n: 0, remain: 0, size: 0, used: 0, minEnd: '', minCreated: '',
    };
    e.n += 1;
    e.remain += Number(p.remain || 0);
    e.size += Number(p.size || 0);
    e.used += Number(p.used || 0);
    const t = (p.end_time || '').slice(0, 10);
    if (t && (!e.minEnd || t < e.minEnd)) e.minEnd = t;
    const c = (p.created_at || '').slice(0, 10);
    if (c && (!e.minCreated || c < e.minCreated)) e.minCreated = c;
    m.set(k, e);
  }
  return [...m.values()].sort((a, b) => b.size - a.size);
}

const PK_DEFAULT_DETAIL_LIMIT = 5;

function pkDetailLimitValue(raw) {
  const n = Number(raw);
  return Number.isFinite(n) && n > 0 ? Math.floor(n) : PK_DEFAULT_DETAIL_LIMIT;
}

function pkDetailLimit(cfg) {
  return pkDetailLimitValue(cfg && cfg.panel && cfg.panel.package_detail_limit);
}

const PK_DAY_MS = 24 * 3600 * 1000;

function pkExpiryMs(p) {
  const raw = Number(p && p.expires_at);
  if (Number.isFinite(raw) && raw > 0) return raw;
  const text = String((p && p.end_time) || '').trim();
  if (!text) return null;
  let iso = text.includes('T') ? text : text.replace(' ', 'T');
  if (!/(?:Z|[+-]\d\d:\d\d)$/.test(iso)) iso += '+08:00';
  const parsed = Date.parse(iso);
  return Number.isFinite(parsed) ? parsed : null;
}

// pkDetailGroups 只服务单账号逐包明细：正余额包先按到期时间挑选默认展示项，
// 其余正余额包与已用完包分别折叠；同一到期时间按面额降序。
function pkDetailCompare(a, b) {
  const sizeOf = p => {
    const n = Number(p && p.size);
    return Number.isFinite(n) ? n : 0;
  };
  const ea = pkExpiryMs(a), eb = pkExpiryMs(b);
  if (ea == null && eb != null) return 1;
  if (ea != null && eb == null) return -1;
  if (ea != null && eb != null && ea !== eb) return ea - eb;
  return sizeOf(b) - sizeOf(a);
}

function pkDetailGroups(packs, limit) {
  const active = [], used = [];
  let usedSize = 0, restSize = 0, restRemain = 0;
  for (const p of packs || []) {
    const remain = Number(p && p.remain);
    if (remain > 0) {
      active.push(p);
      continue;
    }
    used.push(p);
    const size = Number(p && p.size);
    if (Number.isFinite(size)) usedSize += size;
  }
  active.sort(pkDetailCompare);
  used.sort(pkDetailCompare);
  const visible = active.slice(0, pkDetailLimitValue(limit));
  const rest = active.slice(visible.length);
  for (const p of rest) {
    const size = Number(p && p.size);
    if (Number.isFinite(size)) restSize += size;
    const remain = Number(p && p.remain);
    if (Number.isFinite(remain)) restRemain += remain;
  }
  return { visible, rest, used, restSize, restRemain, usedSize };
}

function pkCreditOpacity(days) {
  if (days == null || !Number.isFinite(Number(days))) return 1;
  return 0.25 + 0.75 * Math.max(0, Math.min(29, Number(days) - 1)) / 29;
}

function pkExpiryText(expiresAt) {
  if (!expiresAt) return '无到期时间';
  const diff = expiresAt - Date.now();
  if (diff <= 0) return '已到期';
  const minutes = Math.max(1, Math.ceil(diff / 60000));
  if (minutes < 60) return '剩余 ' + minutes + ' 分钟';
  const hours = Math.ceil(diff / 3600000);
  if (hours < 24) return '剩余 ' + hours + ' 小时';
  return '剩余 ' + Math.ceil(diff / PK_DAY_MS) + ' 天';
}

function pkExpiryDateTime(expiresAt) {
  if (!expiresAt) return '—';
  return new Date(expiresAt).toLocaleString('zh-CN', {
    timeZone: 'Asia/Shanghai', hour12: false,
    year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit',
  });
}

function pkAccountSegments(a, now) {
  let balance = Math.max(0, Number(a.remain || 0));
  const out = [];
  for (const p of a.packages || []) {
    const remain = Number(p.remain || 0);
    if (!Number.isFinite(remain) || remain <= 0 || balance <= 0) continue;
    const amount = Math.min(balance, remain);
    const expiresAt = pkExpiryMs(p);
    out.push({
      amount,
      expiresAt,
      days: expiresAt == null ? null : Math.max(0, Math.ceil((expiresAt - now) / PK_DAY_MS)),
      source: p.name || '积分',
      uid: String(a.uid || ''),
      accountName: a.nickname || String(a.uid || '').slice(0, 8) || '未命名账号',
    });
    balance -= amount;
  }
  return out.sort((x, y) => {
    if (x.expiresAt == null && y.expiresAt != null) return 1;
    if (x.expiresAt != null && y.expiresAt == null) return -1;
    return (x.expiresAt || 0) - (y.expiresAt || 0);
  });
}

// summarizeCreditDays 对齐 WorkDaddy：按精确剩余天数逐行聚合，无有效到期时间的余额
// 不进入图表，也不猜测到期日。账号内先按总余额约束逐包金额，避免上游重复记录膨胀。
function summarizeCreditDays(list, now) {
  const buckets = new Map();
  let unavailable = 0;
  for (const a of list || []) {
    if (a.error || !Number.isFinite(Number(a.remain))) {
      unavailable++;
      continue;
    }
    for (const segment of pkAccountSegments(a, now)) {
      if (segment.days == null) continue;
      let row = buckets.get(segment.days);
      if (!row) {
        row = { days: segment.days, credits: 0, segments: [] };
        buckets.set(segment.days, row);
      }
      row.credits += segment.amount;
      row.segments.push(segment);
    }
  }
  const rows = [...buckets.values()].sort((a, b) => a.days - b.days);
  for (const row of rows) {
    row.segments.sort((a, b) =>
      (a.expiresAt || Infinity) - (b.expiresAt || Infinity) ||
      a.accountName.localeCompare(b.accountName) ||
      a.source.localeCompare(b.source));
  }
  return { rows, accountCount: (list || []).length, unavailable };
}

function renderExpiryDistribution(list, now) {
  const summary = summarizeCreditDays(list, now);
  const colors = pkAccountColorMap(list);
  const rows = summary.rows.map(row => {
    const total = row.credits || 1;
    const nodes = row.segments.map(segment => {
      const color = colors.get(segment.uid) || 'var(--accent)';
      const title = segment.source + '\n' + fmtTok(segment.amount) + ' 积分\n到期时间 ' +
        pkExpiryDateTime(segment.expiresAt) + '（' + pkExpiryText(segment.expiresAt) + '）\n' +
        segment.accountName;
      return '<span class="pk-expiry-seg" style="--seg-color:' + color +
        ';opacity:' + pkCreditOpacity(segment.days).toFixed(5) +
        ';flex:' + Math.max(0.008, segment.amount / total).toFixed(4) +
        ' 1 0" title="' + esc(title) + '" aria-label="' + esc(title) + '"></span>';
    }).join('');
    return '<div class="pk-expiry-row"><span>' + esc(row.days === 0 ? '已到期' : row.days + ' 天') +
      '</span><div class="pk-expiry-track">' + nodes + '</div><b>' + esc(fmtTok(row.credits)) +
      '</b></div>';
  }).join('');
  const foot = summary.accountCount + ' 个账号' +
    (summary.unavailable ? ' · ' + summary.unavailable + ' 个未获取余额' : '');
  const legend = (list || []).filter(a =>
    a && !a.error && a.uid && pkAccountSegments(a, now).some(s => s.days != null)
  ).map(a => '<span><i style="background:' + (colors.get(String(a.uid)) || 'var(--accent)') +
    '"></i>' + esc(a.nickname || String(a.uid).slice(0, 8)) + '</span>').join('');
  const hdr = '<div class="pk-expiry-hdr"><span>剩余天数</span><span style="text-align:center">各账号该批剩余</span><b>剩余积分</b></div>';
  $('pkExpiry').innerHTML = (rows
    ? hdr + '<div class="pk-expiry-chart">' + rows + '</div>'
    : '<div class="pk-expiry-empty">暂无可汇总积分</div>') +
    (legend ? '<div class="pk-expiry-legend">' + legend + '</div>' : '') +
    '<div class="pk-expiry-foot">' + esc(foot) + '</div>';
}

function renderPackages(d, detailLimit) {
  const list = (d.accounts || []);
  const now = Date.now();
  const expiryColors = pkAccountColorMap(list);
  renderExpiryDistribution(list, now);
  if (!list.length) {
    $('pkSummary').innerHTML = '<div class="empty">没有账号</div>';
    return;
  }

  // 包名 → 稳定色号（跨账号一致，方便肉眼对齐）
  const names = [];
  for (const a of list) for (const s of pkBySource(a.packages || [])) {
    if (!names.includes(s.key)) names.push(s.key);
  }
  names.sort((x, y) => {
    const sz = n => Math.max(...list.map(a => {
      const f = pkBySource(a.packages || []).find(s => s.key === n);
      return f ? f.size : 0;
    }));
    return sz(y) - sz(x);
  });
  const colorOf = n => pkColor(names.indexOf(n));
  // 键 → 展示名，供卡片与明细表共用（同一来源必然同色同名）。
  const labelOf = {};
  for (const a of list) for (const s of pkBySource(a.packages || [])) labelOf[s.key] = s;

  const maxRemain = Math.max(1, ...list.map(a => Number(a.remain || 0)));

  $('pkSummary').innerHTML = list.map(a => {
    if (a.error) {
      return '<div class="pk-card"><div class="who"><span class="nm">' +
        esc((a.nickname || a.uid.slice(0, 8))) + '</span>' +
        '<span class="realm">' + esc(a.realm || '') + '</span></div>' +
        '<div class="err">查询失败：' + esc(a.error) + '</div></div>';
    }
    const srcs = pkBySource(a.packages || []);
    const total = Math.max(1, Number(a.size || 0));
    const bar = srcs.map(s =>
      '<i style="width:' + (s.size / total * 100).toFixed(2) + '%;background:' +
      colorOf(s.key) + '" title="' + esc(s.name) + ' ' + fmtTok(s.size) + '"></i>'
    ).join('');
    const legend = srcs.map(s =>
      '<span><i style="background:' + colorOf(s.key) + '"></i>' +
      esc(s.name.replace(/^CodeBuddy/, '')) + ' x' + s.n + ' · ' + fmtTok(s.size) +
      (s.minCreated ? ' · 首发 ' + esc(s.minCreated.slice(5)) : '') + '</span>'
    ).join('');
    const expiry = pkAccountSegments(a, now);
    const expiryTotal = Math.max(1, expiry.reduce((sum, s) => sum + s.amount, 0));
    const expiryColor = expiryColors.get(String(a.uid)) || 'var(--accent)';
    const expiryBar = expiry.length ? '<div class="expirybar" role="img" aria-label="积分到期分布">' +
      expiry.map(s => {
        const title = s.source + '\n' + fmtTok(s.amount) + ' 积分\n到期时间 ' +
          pkExpiryDateTime(s.expiresAt) + '（' + pkExpiryText(s.expiresAt) + '）';
        return '<i style="background:' + expiryColor +
          ';opacity:' + pkCreditOpacity(s.days).toFixed(5) +
          ';flex:' + Math.max(0.008, s.amount / expiryTotal).toFixed(4) +
          ' 1 0" title="' + esc(title) + '"></i>';
      }).join('') + '</div>' : '';
    return '<div class="pk-card">' +
      '<div class="who"><span class="nm">' + esc(a.nickname || a.uid.slice(0, 8)) + '</span>' +
      '<span class="realm">' + esc(a.realm || '') + '</span></div>' +
      '<div class="big">' + fmtTok(a.remain) + '</div>' +
      '<div class="sub">共 ' + fmtTok(a.size) + ' · ' + (a.packages || []).length +
      ' 个包 · 占最高 ' + (Number(a.remain || 0) / maxRemain * 100).toFixed(0) + '%</div>' +
      '<div class="mixbar">' + bar + '</div>' +
      expiryBar +
      '<div class="pk-legend">' + legend + '</div>' +
      '</div>';
  }).join('');

  $('pkNote').textContent = list.length + ' 个账号 · 实时查询上游';

  // 逐包明细：每个账号一个表，包的**面额**列是重点
  $('pkDetail').innerHTML = list.map(a => {
    if (a.error) return '';
    const groups = pkDetailGroups(a.packages || [], detailLimit);
    const rowOf = (p, rowGroup) => {
      const k = (p.package_code || '') + '|' + (p.name || '(未命名)');
      const sub = (p.sub_product_code || '').replace(/^sp_tcaca_codebuddyide_?/, '') ||
                  (p.package_code || '').replace(/^TCACA_/, '');
      return '<tr' + (rowGroup ? ' class="pk-hidden-row pk-' + rowGroup +
        '-row" data-pk-row="' + rowGroup + '" hidden' : '') +
        '><td class="mark" aria-hidden="true"><i style="background:' +
        colorOf(k) + '"></i></td>' +
      '<td>' + esc(p.name || '(未命名)') +
        (sub ? '<div class="note">' + esc(sub) + '</div>' : '') + '</td>' +
      '<td class="num">' + fmtTok(p.size) + '</td>' +
      '<td class="num">' + fmtTok(p.remain) + '</td>' +
      '<td class="num">' + fmtTok(p.used) + '</td>' +
      '<td class="num">' + esc((p.created_at || '').slice(0, 16).replace('T', ' ') || '—') + '</td>' +
      '<td class="num">' + esc((p.end_time || '').slice(0, 10) || '—') + '</td>' +
      '</tr>';
    };
    const groupSummary = (group, label, count, size, remain) =>
      '<tr class="pk-group-summary"><td colspan="7"><button type="button" class="pk-group-toggle"' +
      ' data-pk-group="' + group + '" data-count="' + count + '" data-size="' + size +
      '" data-remain="' + remain + '" aria-expanded="false">' + label + '，展开</button></td></tr>';
    const rows = groups.visible.map(p => rowOf(p, '')).join('');
    const restSummary = groups.rest.length
      ? groupSummary('rest', '其余未用完 ' + groups.rest.length + ' 个包（面额合计 ' +
          fmtTok(groups.restSize) + ' · 剩余 ' + fmtTok(groups.restRemain) + '）',
          groups.rest.length, groups.restSize, groups.restRemain) +
        groups.rest.map(p => rowOf(p, 'rest')).join('')
      : '';
    const usedSummary = groups.used.length
      ? groupSummary('used', '已用完 ' + groups.used.length + ' 个包（面额合计 ' +
          fmtTok(groups.usedSize) + '）', groups.used.length, groups.usedSize, 0) +
        groups.used.map(p => rowOf(p, 'used')).join('')
      : '';
    return '<div class="box"><header><h3>' +
      esc(a.nickname || a.uid.slice(0, 8)) + ' · ' + esc(a.realm || '') +
      '</h3><span class="grow"></span><span class="note">余额 ' + fmtTok(a.remain) +
      ' / 总额 ' + fmtTok(a.size) + ' · 可用 ' + (groups.visible.length + groups.rest.length) + ' 个包' +
      (groups.used.length ? ' / 已用完 ' + groups.used.length + ' 个' : '') +
      ' · 默认展示最早到期 ' + pkDetailLimitValue(detailLimit) + ' 条</span>' +
      '</header><div class="tbl-wrap"><table class="acc"><thead><tr>' +
      '<th class="mark" aria-hidden="true"></th><th>包名 / 来源</th>' +
      '<th class="num">面额</th><th class="num">剩余</th><th class="num">已用</th>' +
      '<th class="num">发放</th><th class="num">到期</th>' +
      '</tr></thead><tbody>' + rows + restSummary + usedSummary + '</tbody></table></div></div>';
  }).join('');
}

if ($('pkDetail')) $('pkDetail').addEventListener('click', ev => {
  const btn = ev.target.closest('button[data-pk-group]');
  if (!btn) return;
  const body = btn.closest('tbody');
  if (!body) return;
  const group = btn.dataset.pkGroup;
  const expanded = btn.getAttribute('aria-expanded') === 'true';
  body.querySelectorAll('tr[data-pk-row="' + group + '"]').forEach(row => { row.hidden = expanded; });
  const count = btn.dataset.count || '0';
  const size = btn.dataset.size || '0';
  const remain = btn.dataset.remain || '0';
  btn.setAttribute('aria-expanded', String(!expanded));
  if (group === 'rest') {
    btn.textContent = expanded
      ? '其余未用完 ' + count + ' 个包（面额合计 ' + fmtTok(size) + ' · 剩余 ' +
        fmtTok(remain) + '），展开'
      : '收起其余未用完 ' + count + ' 个包';
  } else {
    btn.textContent = expanded
      ? '已用完 ' + count + ' 个包（面额合计 ' + fmtTok(size) + '），展开'
      : '收起已用完 ' + count + ' 个包';
  }
});

async function loadPackages() {
  $('pkSummary').innerHTML = '<div class="empty">查询中…（逐账号向上游实时查询）</div>';
  $('pkDetail').innerHTML = '';
  $('pkExpiry').innerHTML = '<div class="pk-expiry-empty">查询中…</div>';
  try {
    const [d, c] = await Promise.all([
      api('packages'),
      api('config').catch(() => null),
    ]);
    renderPackages(d, pkDetailLimit(c && c.config));
  } catch (e) {
    $('pkSummary').innerHTML = '<div class="empty">读取失败：' + esc(e.message) + '</div>';
    $('pkExpiry').innerHTML = '<div class="pk-expiry-empty">读取失败：' + esc(e.message) + '</div>';
  }
}

if ($('btnPk')) $('btnPk').onclick = loadPackages;
