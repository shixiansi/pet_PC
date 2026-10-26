// 验证 import-codebuddy-accounts.ps1 的核心算法（字段映射 / 分渠道 / 去重 / ref 命名），
// 对着真实的快照库与池子文件跑一遍，只读不写。
const fs = require('node:fs')
const path = require('node:path')
const os = require('node:os')

const DSH = process.env.DSH_HOME || path.join(os.homedir(), '.dsh')
const SNAP = path.join(DSH, 'codebuddy-accounts')
const CREDS = path.join(DSH, '.credentials.yaml')
const STATE = path.join(DSH, 'channel-pack', 'state.json')

const DomainToProvider = {
  'www.codebuddy.cn': 'buddy',
  'copilot.tencent.com': 'buddy',
  'www.workbuddy.ai': 'workbuddy',
}

// --- 复刻脚本里 refs: 段的定位与解析 ---
const lines = fs.readFileSync(CREDS, 'utf8').split(/\r?\n/)
let refsIdx = -1
for (let i = 0; i < lines.length; i++) if (/^refs:\s*$/.test(lines[i])) { refsIdx = i; break }
if (refsIdx < 0) throw new Error('找不到 refs: 段')
let refsEnd = lines.length
for (let i = refsIdx + 1; i < lines.length; i++) if (/^\S/.test(lines[i])) { refsEnd = i; break }

// 忠实复刻脚本的解析：键行 + 其后续缩进行整体拼接，再正则取 user_id。
// （DSH 写进去的凭据值可能跨行，只取键行会漏掉 user_id，去重就失效。）
function credentialUserId(raw) {
  const m = /"user_id"\s*:\s*"([^"]*)"/.exec(raw)
  return m ? m[1] : ''
}

const existingRefs = new Map()
let curName = null
let curBuf = []
for (let i = refsIdx + 1; i < refsEnd; i++) {
  const m = /^\s+([A-Za-z][A-Za-z0-9_]*):\s?(.*)$/.exec(lines[i])
  if (m) {
    if (curName) existingRefs.set(curName, credentialUserId(curBuf.join('\n')))
    curName = m[1]
    curBuf = [m[2]]
  } else if (curName) {
    curBuf.push(lines[i].trim())
  }
}
if (curName) existingRefs.set(curName, credentialUserId(curBuf.join('\n')))

const existingUids = new Map()
for (const [ref, uid] of existingRefs) if (uid) existingUids.set(uid, ref)

const state = JSON.parse(fs.readFileSync(STATE, 'utf8'))
console.log(`DSH home : ${DSH}`)
console.log(`池内账号 : ${state.accounts.length}`)
for (const a of state.accounts) console.log(`  - ${a.provider} / ${a.nickname} / ${existingRefs.get(a.credentialRef) || '?'}`)
console.log('')

// --- 收集待导入 ---
const files = fs.readdirSync(SNAP).filter(f => f.endsWith('.json') && f !== 'index.json').sort()
const plan = [], skipped = []
for (const f of files) {
  let snap
  try { snap = JSON.parse(fs.readFileSync(path.join(SNAP, f), 'utf8')) }
  catch (e) { skipped.push([f, `JSON 解析失败: ${e.message}`]); continue }

  const accessToken = String(snap.auth?.accessToken ?? '')
  const refreshToken = String(snap.auth?.refreshToken ?? '')
  const uid = String(snap.account?.uid ?? '')
  let nickname = String(snap.account?.nickname ?? '')
  const domain = String(snap.auth?.domain ?? '')

  if (!accessToken) { skipped.push([f, '缺 auth.accessToken']); continue }
  if (!refreshToken) { skipped.push([f, '缺 auth.refreshToken（池子会抛异常）']); continue }
  if (!uid) { skipped.push([f, '缺 account.uid']); continue }
  if (!nickname) nickname = uid.slice(0, 8)

  const provider = DomainToProvider[domain]
  if (!provider) { skipped.push([f, `domain '${domain}' 不属于任何渠道`]); continue }
  if (existingUids.has(uid)) { skipped.push([f, `uid 已在池中（${existingUids.get(uid)}），跳过以免重复`]); continue }

  const suffix = uid.replace(/-/g, '').slice(0, 8).toUpperCase()
  const refName = `${provider.toUpperCase()}_ACCOUNT_${suffix}`
  if (existingRefs.has(refName)) { skipped.push([f, `ref 名 ${refName} 已存在`]); continue }
  // 正则校验：pack.js 的 ACCOUNT_REF_RE
  if (!/^(CODEARTS|BUDDY|WORKBUDDY|LOBSTERAI|QODER|QODERCN|TRAE|CLINE|LOOMY|RACCOON|MINIMAX|ZCODE|OPENCODE|GEMINI)_ACCOUNT_([0-9A-Fa-f]{6,})$/.test(refName)) {
    skipped.push([f, `ref 名 ${refName} 不符合 pack.js 正则`]); continue
  }

  const credential = {
    access_token: accessToken,
    refresh_token: refreshToken,
    expires_at: snap.auth.expiresAt,
    refresh_expires_at: snap.auth.refreshExpiresAt,
    token_type: 'Bearer',
    scope: '',
    domain,
    user_id: uid,
    nickname,
    enterprise_id: '',
    account_type: 'personal',
  }
  plan.push({ f, provider, nickname, uid, domain, refName, accountId: `${provider}-${suffix.toLowerCase()}`, credential })
}

console.log(`待导入 ${plan.length} 个：`)
for (const p of plan) {
  const shown = JSON.stringify({ ...p.credential, access_token: '<' + p.credential.access_token.length + ' chars>', refresh_token: '<' + p.credential.refresh_token.length + ' chars>' })
  console.log(`  + ${p.provider} / ${p.nickname} / ${p.refName} / ${p.domain}`)
  console.log(`      ${shown}`)
}
console.log('')
console.log(`跳过 ${skipped.length} 个：`)
for (const [f, why] of skipped) console.log(`  - ${f}：${why}`)

// --- 断言：池子侧消费这些凭据的假设 ---
console.log('\n== 池子侧假设校验 ==')
for (const p of plan) {
  const c = p.credential
  const refreshable = typeof c.refresh_token === 'string' && c.refresh_token.length > 0
  const expOk = Number.isFinite(c.expires_at)
  console.log(`  ${p.refName}: isRefreshable=${refreshable} expiresAt=${new Date(c.expires_at).toISOString()} ${expOk ? '' : '(!! 非数字)'}`)
}
