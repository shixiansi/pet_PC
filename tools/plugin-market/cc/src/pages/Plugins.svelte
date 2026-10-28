<script lang="ts">
  import PluginFrame from '../components/PluginFrame.svelte';
  import {
    Installed,
    Grant,
    SetEnabled,
    PermissionLabels,
    SelectedID,
    RememberSelection,
    GetConfig,
    SaveConfig,
  } from '../lib/bridge';
  import type { PluginRow, PluginConfig, ConfigEditor } from '../lib/bridge';

  // The row type is the GENERATED one (wailsjs/go/models.ts -> plugins.Installed), not a
  // hand-written description of it. That is the whole fix: this page used to declare
  // `{ id, version, enabled, granted, manifest }` in camelCase, the host sends Go field
  // names (`ID`, `Enabled`, `Manifest`), and both sides were wrong in a way that only
  // looked empty at runtime. Now a field the host does not send is a `npm run check`
  // error instead of four blank rows.
  let rows = $state<PluginRow[]>([]);
  let labels = $state<Record<string, string>>({});
  let loaded = $state(false);
  let error = $state('');

  // Which plugin the detail side shows, and whether the list column is folded away so its
  // interface gets the whole page. Both are page state, not host state: the selection is
  // remembered through SelectedID/RememberSelection (that is what those bindings are for),
  // the expansion is not remembered at all - a window that reopens "already expanded" is a
  // window whose layout the user did not choose this time.
  let selectedId = $state('');
  let expanded = $state(false);
  /** The selection as of the last run, so a refresh does not fight the user's click. */
  let remembered = '';

  const selected = $derived(rows.find((r) => r.ID === selectedId) ?? null);
  const grantedCount = $derived((selected?.Granted ?? []).length);
  const permCount = $derived(selected?.Manifest?.permissions?.length ?? 0);

  // --- the plugin's own settings (manifest.config) -----------------------------------
  //
  // A MODAL, and that is the conclusion the old window reached after trying the other two
  // shapes (`docs/plugin-market.md` §2.3): a settings panel in the flow pushes the plugin's
  // interface down, and the interface is the reason this page exists. The dialog also survives
  // a wide form (12 items) without stealing the frame's height.
  //
  // `values` is the editable copy, keyed by config key; `cfg` is what the host said the items
  // are. They are separate because a secret's value is NEVER in `cfg` - see seedValues.
  let cfg = $state<PluginConfig | null>(null);
  let cfgOpen = $state(false);
  let cfgBusy = $state(false);
  let cfgError = $state('');
  let values = $state<Record<string, string>>({});
  let toastText = $state('');

  /** Only a plugin that DECLARED settings gets an entry: a gear that opens nothing is a lie. */
  const hasConfig = $derived((selected?.Manifest?.config?.length ?? 0) > 0);

  // Started here rather than in onMount. onMount is one more thing between "the page
  // rendered" and "the request started", and a page that renders but never starts is
  // exactly the failure this page had: a permanent 读取中 with no error and no console
  // message. Assigning $state during init is allowed, so there is nothing to defer.
  //
  // The remembered selection is read FIRST and separately: it only decides what is selected,
  // and it must not be able to hold the list hostage (`withTimeout` would let a stuck binding
  // delay the rows by four seconds).
  void boot();
  async function boot() {
    try {
      remembered = await withTimeout('读取上次选中', SelectedID());
    } catch {
      /* not fatal: the page just opens with nothing selected */
    }
    await refresh();
  }

  // Every binding call is awaited SEPARATELY, behind a timeout, and the reason is the bug
  // this page shipped with: it sat on "读取中…" forever.
  //
  // A Wails binding that never settles (a panic on the Go side settles nothing) leaves an
  // await pending for the life of the window, and `finally` never runs. A page whose only
  // two states are "reading" and "ready" then lies: it reports progress it is not making.
  // So: name what failed, and refuse to be silent for longer than a couple of seconds.
  function withTimeout<T>(what: string, p: Promise<T>, ms = 4000): Promise<T> {
    return Promise.race([
      p,
      new Promise<T>((_, reject) => setTimeout(() => reject(new Error(what + ' 超过 ' + ms + 'ms 没有回应')), ms)),
    ]);
  }

  // A Wails rejection is not always an Error: the Go side can reject with a bare string,
  // and a structured one prints as "[object Object]" through String(). Both of those have
  // already cost a round trip in this page's history, so print what is actually there.
  function why(e: unknown): string {
    if (e instanceof Error) return e.message;
    if (typeof e === 'string') return e;
    return peek(e);
  }

  // Print the payload, not the idea of it. Anything non-string goes through JSON, because
  // String() on an object gives "[object Object]" - the value was there and nothing showed
  // it, which is how a status code survived several rounds of guessing.
  function peek(v: unknown): string {
    try {
      const s = JSON.stringify(v);
      if (s === undefined) return String(v);
      return s.length > 400 ? s.slice(0, 400) + '…' : s;
    } catch {
      return String(v);
    }
  }

  async function refresh() {
    const problems: string[] = [];
    try {
      const list = await withTimeout('读取已装插件', Installed());
      if (list == null) rows = [];
      else if (!Array.isArray(list)) throw new Error('宿主返回的不是数组，原样：' + peek(list));
      else rows = list;
    } catch (e) {
      problems.push('已装列表：' + why(e));
    }
    try {
      // The labels come from the host, not from a map in this file: the install dialog is
      // the user's consent, and consent described two ways is consent to two things.
      // A slice of {ID, Label} rather than a map, because a map arrives as an anonymous
      // object that nothing describes - see the comment on LabelPair in the Go side.
      const pairs = await withTimeout('读取权限文案', PermissionLabels());
      if (pairs == null) {
        labels = {};
      } else if (!Array.isArray(pairs)) {
        // Naming this case is the point: a map, an error object or a bare string all used
        // to reach the lookup below and fail as "x.map is not a function".
        throw new Error('宿主返回的不是 {ID, Label} 数组，原样：' + peek(pairs));
      } else {
        labels = Object.fromEntries(pairs.map((p) => [p.ID, p.Label]));
      }
    } catch (e) {
      // Not fatal: the page can still show ids, and saying which call failed is worth
      // more than a blank screen.
      problems.push('权限文案：' + why(e));
    }

    // The selection survives a refresh; a plugin that is no longer on disk does not. The
    // remembered id is only a STARTING point - once the user has clicked, their click wins.
    if (selectedId && !rows.some((r) => r.ID === selectedId)) {
      selectedId = '';
      expanded = false;
      closeConfig();
    }
    if (!selectedId && remembered && rows.some((r) => r.ID === remembered)) {
      selectedId = remembered;
    }

    error = problems.join('；');
    loaded = true;
  }

  function name(r: PluginRow | null): string {
    if (!r) return '';
    return r.Manifest?.name || r.ID || '（这一行没有 ID）';
  }

  function label(perm: string): string {
    return labels[perm] || perm;
  }

  /**
   * Whether this plugin's own interface can be hosted here at all.
   *
   * The gate is "installed, enabled, it ships a document, and that document is not laid out
   * relative to the pet".
   *
   * The last clause is what keeps a floating-surface plugin out of this page. `surface` blocks are
   * coordinates measured from the PET's top-left corner, and this window has no pet: mounting such a
   * document here gives it rectangles it cannot honour, and - the case that was reported - a
   * document that talks to its own other frame through `plugin.share`, which this program does not
   * serve. The user saw 「插件 com.shixiansi.plugin-pomodoro 调用了本程序不支持的方法 share.set」
   * in the middle of the plugin page. The fix is not to teach this program a pet-shaped channel; it
   * is to stop mounting a document that was never meant for this window.
   *
   * The three bundled plugins that DO need this page declare `panel: false`, no `surface` and
   * `background: true` precisely because their interface lives HERE (see
   * openspec/changes/plugin-page-workspace). A disabled plugin gets no frame either: switching a
   * plugin off has to stop it running, not just stop it being listed.
   */
  function mountable(r: PluginRow | null): boolean {
    if (!r || !r.Manifest || !r.Enabled || !r.Manifest.entry) return false;
    const surface = r.Manifest.surface;
    return !(surface && surface.length > 0);
  }

  /** Why there is no interface, in the user's words - four different things to do. */
  function noInterfaceWhy(r: PluginRow | null): string {
    if (!r) return '';
    if (r.Error) return '界面不可用：' + r.Error;
    if (!r.Manifest) return '这个插件的 manifest 读不出来，界面没有加载。';
    if (!r.Enabled) return '插件已停用，界面不运行。打开「启用」它才会跑。';
    if (!r.Manifest.entry) return '这个插件没有声明界面（manifest 里没有 entry）。';
    const surface = r.Manifest.surface;
    if (surface && surface.length > 0) {
      return '这个插件的界面是桌宠旁边的浮层面，只能在桌宠上打开（右键桌宠 → 番茄钟 / 日程 / 对话）。';
    }
    return '';
  }

  function select(r: PluginRow) {
    if (selectedId === r.ID) return;
    selectedId = r.ID;
    // A different plugin is a different document: the expansion, the permission panel and the
    // settings dialog all belonged to the previous one - a form left open over a plugin the
    // user is no longer looking at would save into the wrong plugin's directory.
    expanded = false;
    closeConfig();
    // Remembered for the next time the window opens. A failure is not worth a banner: the
    // page works, it just will not remember.
    void RememberSelection(r.ID).catch(() => {});
  }

  // --- the settings dialog ---------------------------------------------------------

  /**
   * Opens the editor for the selected plugin.
   *
   * The dialog opens even when the read fails: the failure is the thing the user needs to
   * read, and a button that silently does nothing is the worse answer (it is the same lesson
   * as 「读取中…」 forever).
   */
  async function openConfig() {
    const r = selected;
    if (!r) return;
    cfgError = '';
    cfgBusy = true;
    try {
      const read = await withTimeout('读取插件设置', GetConfig(r.ID));
      cfg = read ?? null;
      values = seedValues(read);
      cfgOpen = true;
    } catch (e) {
      cfg = null;
      values = {};
      cfgError = why(e);
      cfgOpen = true;
    } finally {
      cfgBusy = false;
    }
  }

  function closeConfig() {
    cfgOpen = false;
    cfgError = '';
  }

  /**
   * The editable starting values, one per item.
   *
   * A SECRET starts EMPTY and stays empty until the user types: the host sends `value: ''`
   * plus a mask, so there is nothing to show - and writing the mask back on save would
   * replace the stored key with bullet characters. Blank therefore means "keep what is
   * stored", which is what the note under the field says.
   *
   * Everything else starts at its stored value, falling back to the declared default: an
   * empty box next to 「模型」 reads as "not set", while the truth is "the default applies".
   */
  function seedValues(read: PluginConfig): Record<string, string> {
    const out: Record<string, string> = {};
    for (const item of read?.items ?? []) {
      if (item.type === 'secret') out[item.key] = '';
      else out[item.key] = String(item.value || item.default || '');
    }
    return out;
  }

  /**
   * The payload, in the shape the host stores.
   *
   * Two rules carried over from the JS window, both of them about not lying:
   *   - a bool is always sent (`true`/`false`), because "unchecked" IS the value;
   *   - an untouched SECRET is not sent at all, because absent means "keep";
   *   - an empty field falls back to its declared default, so the stored value cannot drift
   *     away from what the box shows.
   */
  function collectConfig(): Record<string, string> {
    const out: Record<string, string> = {};
    for (const item of cfg?.items ?? []) {
      const v = values[item.key] ?? '';
      if (item.type === 'bool') out[item.key] = v === 'true' ? 'true' : 'false';
      else if (item.type === 'secret') {
        if (v !== '') out[item.key] = v;
      } else if (v !== '') out[item.key] = v;
      else if (item.default) out[item.key] = item.default;
    }
    return out;
  }

  async function saveConfig() {
    const r = selected;
    if (!r || !cfg) return;
    cfgBusy = true;
    cfgError = '';
    try {
      await SaveConfig(r.ID, collectConfig());
      // Saving closes the dialog: the form WAS the detour, and a dialog that stays open after
      // 「保存」 makes the user close what they already finished with. A failure keeps it open,
      // because there is something in it to fix.
      closeConfig();
      cfg = null;
      await refresh();
      flash('设置已保存');
    } catch (e) {
      cfgError = why(e);
    } finally {
      cfgBusy = false;
    }
  }

  /** A self-clearing message. There is no toast component in this window yet. */
  let flashTimer: ReturnType<typeof setTimeout> | undefined;
  function flash(text: string) {
    toastText = text;
    clearTimeout(flashTimer);
    flashTimer = setTimeout(() => (toastText = ''), 2500);
  }

  /**
   * A `select` with `multiple` is stored as the chosen values joined by commas - the shape the
   * plugins already parse with a `split`, so the editor does not invent a second encoding.
   */
  function multiHas(current: string | undefined, value: string): boolean {
    return String(current ?? '')
      .split(/[,，\s]+/)
      .filter(Boolean)
      .includes(value);
  }

  function toggleMulti(current: string | undefined, value: string, on: boolean): string {
    const set = new Set(
      String(current ?? '')
        .split(/[,，\s]+/)
        .filter(Boolean),
    );
    if (on) set.add(value);
    else set.delete(value);
    return [...set].join(',');
  }

  /**
   * Whether one control wants the whole width of the grid.
   *
   * Decided from the ITEM's own shape, never from a list of key names, so a plugin that adds a
   * long field gets a sensible layout without anyone editing this page - and no plugin's
   * private key names are hard-coded into the window that merely edits them.
   */
  function fieldWantsFullWidth(item: ConfigEditor): boolean {
    // A checkbox group wraps: give it the row instead of squeezing the labels.
    if (item.type === 'select') return true;
    // These carry a sentence underneath, and half a cell wraps it to three lines.
    if (item.type === 'secret' || item.hostWhitelist) return true;
    if (item.type === 'text') {
      // 14 characters is roughly where a half-width cell starts truncating a path or a URL.
      const label = String(item.label || '');
      const value = String(item.value || item.default || '');
      return label.length > 14 || value.length > 20;
    }
    return false;
  }

  // The grant list is replaced WHOLE on every toggle: the host's Grant takes the complete
  // set, so sending a delta would be a second definition of what "granted" means.
  async function togglePerm(r: PluginRow, perm: string, on: boolean) {
    const granted = new Set(r.Granted ?? []);
    if (on) granted.add(perm);
    else granted.delete(perm);
    await act(() => Grant(r.ID, [...granted]));
  }

  async function toggleEnabled(r: PluginRow, on: boolean) {
    await act(() => SetEnabled(r.ID, on));
  }

  async function act(run: () => Promise<unknown>) {
    try {
      await run();
      await refresh();
    } catch (e) {
      error = why(e);
    }
  }

  function onKeydown(event: KeyboardEvent) {
    if (event.key !== 'Escape') return;
    // The dialog is ON TOP, so it is what Escape closes first. Collapsing the list while a
    // modal is up would leave the modal over a layout it no longer matches.
    if (cfgOpen) {
      closeConfig();
      return;
    }
    if (expanded) expanded = false;
  }
</script>

<svelte:window onkeydown={onKeydown} />

<div class="page" class:expanded>
  <!-- The list column. Folded away by 「展开」 so the plugin's own document gets the whole
       page - the voice workshop lays out two columns (a character list beside five lines of
       takes) down to 720px and collapses to one below that, and a split pane in this window
       is ~510px. Folding the list is enough to cross that line; the rail keeps its 168px
       because the page cannot reach across App.svelte for 168px it does not need. -->
  {#if !expanded}
    <nav class="list" aria-label="已装插件">
      <!-- `· v5` is a BUILD STAMP, and it is not decoration.
           This program embeds its interface at COMPILE time (`//go:embed all:frontend/dist`),
           so rebuilding the frontend alone changes nothing the user can see - the window
           keeps running the previous bundle. That exact trap has now cost this project a
           round twice: once when the master-detail page was written and the exe was not
           rebuilt (「插件页面又没有了」), and once by the plugin market's own README round
           (`docs/plugin-market.md` §6.1). A number on screen answers "did my rebuild land?"
           in one glance instead of a `Get-Item` comparison - the page it came from carried
           `已装插件 · v4` for the same reason, and this rewrite dropped it.
           Bump the number when the page's shape changes, not on every edit. -->
      <p class="list-head">
        <span>已装插件 · v5</span>
        <span class="count">{loaded ? rows.length + ' 个' : ''}</span>
      </p>
      {#if !loaded}
        <p class="hint">读取中…</p>
      {:else if rows.length === 0}
        <p class="hint">还没有插件。安装在这里还没有做（见 tasks.md 阶段 3），现在先去市场装。</p>
      {:else}
        {#each rows as r, ri (r.ID + ':' + ri)}
          <button
            type="button"
            class="row"
            class:selected={r.ID === selectedId}
            class:is-off={!r.Enabled}
            class:is-err={Boolean(r.Error)}
            onclick={() => select(r)}
            title={r.ID}
          >
            <span class="dot" class:on={r.Enabled && !r.Error} class:err={Boolean(r.Error)}></span>
            <span class="row-name">{name(r)}</span>
            <span class="row-meta">v{r.Version || '?'}</span>
          </button>
        {/each}
      {/if}
    </nav>
  {/if}

  <section class="detail">
    {#if !selected}
      <p class="hint empty-pick">
        {#if !loaded}
          读取中…
        {:else if rows.length === 0}
          还没有装任何插件。
        {:else}
          从左边选一个插件。选中之后，插件自己的界面会显示在这里——它跑在沙箱里，只拿到你勾选的权限。
        {/if}
      </p>
    {:else}
      <header class="identity">
        <div class="who">
          <h2>{name(selected)}</h2>
          <span class="meta" title={selected.Dir || selected.ID}>
            {selected.ID} · v{selected.Version || '?'}{selected.InstalledAt ? ' · 装于 ' + selected.InstalledAt.slice(0, 10) : ''}
          </span>
        </div>

        <div class="ops">
          <label class="switch" title="启用或停用这个插件">
            <input
              type="checkbox"
              checked={selected.Enabled}
              onchange={(e) => toggleEnabled(selected as PluginRow, (e.currentTarget as HTMLInputElement).checked)}
            />
            <span>启用</span>
          </label>

          <!-- Permissions are a POPOVER, not a panel: an expanded panel pushes the plugin's
               interface down, and the interface is the reason this page exists
               (docs/plugin-market.md §2.3 reached the same conclusion the hard way).
               <details> rather than a positioned dialog on purpose - closed, it occupies no
               layout, and its content stays in the DOM, which is what lets the render smoke
               assert that the labels come from the host without simulating a click (jsdom
               does not implement <details>'s toggle on click anyway). -->
          <details class="perm-drop">
            <summary class="chip" title="查看并调整这个插件的权限">
              权限 <b>{grantedCount}/{permCount}</b>
            </summary>
            <div class="perm-panel">
              {#if permCount === 0}
                <p class="hint">这个插件没有声明任何权限。</p>
              {:else}
                <ul class="perms">
                  {#each selected.Manifest?.permissions ?? [] as perm, pi (perm + ':' + pi)}
                    <li>
                      <label class="perm" title={perm}>
                        <input
                          type="checkbox"
                          checked={(selected?.Granted ?? []).includes(perm)}
                          onchange={(e) => togglePerm(selected as PluginRow, perm, (e.currentTarget as HTMLInputElement).checked)}
                        />
                        <span>{label(perm)}</span>
                      </label>
                    </li>
                  {/each}
                </ul>
                <p class="hint">
                  勾选即授权。取消勾选会让用到它的方法与请求立刻开始被拒绝；改完界面会按新权限重启。
                </p>
              {/if}
            </div>
          </details>

          {#if hasConfig}
            <!-- Only for a plugin that DECLARED settings. A gear that opens an empty form is
                 the same defect as a permission switch that gates nothing, and the page has
                 been bitten by that class of control before (AGENTS.md §3.1). -->
            <button type="button" class="chip" onclick={openConfig} title="这个插件声明的设置项">
              设置
            </button>
          {/if}

          {#if mountable(selected)}
            <button type="button" class="chip" onclick={() => (expanded = !expanded)} title="让插件界面占满这一页（Esc 收起）">
              {expanded ? '收起列表' : '展开界面'}
            </button>
          {/if}
        </div>
      </header>

      {#if selected.Error}
        <p class="err">manifest 有问题：{selected.Error}</p>
      {/if}

      {#if mountable(selected)}
        <div class="frame-area">
          <PluginFrame inst={selected} />
        </div>
      {:else}
        <p class="hint no-interface">{noInterfaceWhy(selected)}</p>
      {/if}
    {/if}

    {#if error}
      <p class="err">{error}</p>
    {/if}
  </section>
</div>

<!-- The settings dialog. A MODAL, for the reason in the script: an in-flow settings panel
     pushes the plugin's own interface down, and that interface is what this page is for.
     It lives OUTSIDE `.page`'s grid so it cannot be squeezed by the columns, and it is
     `position: fixed` so neither the list's nor the detail's scroll containers can clip it. -->
{#if cfgOpen}
  <div class="sheet" role="dialog" aria-modal="true" aria-label="插件设置">
    <div class="sheet-card">
      <header class="sheet-head">
        <h3>{name(selected)} · 设置</h3>
        <button type="button" class="chip" onclick={closeConfig}>关闭</button>
      </header>

      {#if cfgError}
        <p class="err sheet-note">{cfgError}</p>
      {/if}
      {#if cfg?.error}
        <p class="err sheet-note">{cfg.error}</p>
      {/if}
      {#if cfg?.usage}
        <p class="hint sheet-note">{cfg.usage}</p>
      {/if}

      <div class="cfg">
        {#each cfg?.items ?? [] as item (item.key)}
          <div class="field" class:wide={fieldWantsFullWidth(item)}>
            <span class="field-label" id={'cfg-lab-' + item.key}>{item.label || item.key}</span>

            {#if item.type === 'bool'}
              <input
                type="checkbox"
                aria-labelledby={'cfg-lab-' + item.key}
                checked={values[item.key] === 'true'}
                onchange={(e) => (values[item.key] = (e.currentTarget as HTMLInputElement).checked ? 'true' : 'false')}
              />
            {:else if item.type === 'select' && item.multiple}
              <!-- A checkbox group; the stored value stays the comma-joined string the
                   plugins already parse. -->
              <div class="checks" role="group" aria-labelledby={'cfg-lab-' + item.key}>
                {#each item.options ?? [] as opt (opt.value)}
                  <label class="check">
                    <input
                      type="checkbox"
                      value={opt.value}
                      checked={multiHas(values[item.key], opt.value)}
                      onchange={(e) =>
                        (values[item.key] = toggleMulti(values[item.key], opt.value, (e.currentTarget as HTMLInputElement).checked))}
                    />
                    <span>{opt.label || opt.value}</span>
                  </label>
                {/each}
              </div>
            {:else if item.type === 'select'}
              <select
                aria-labelledby={'cfg-lab-' + item.key}
                value={values[item.key] ?? ''}
                onchange={(e) => (values[item.key] = (e.currentTarget as HTMLSelectElement).value)}
              >
                {#each item.options ?? [] as opt (opt.value)}
                  <option value={opt.value}>{opt.label || opt.value}</option>
                {/each}
              </select>
            {:else if item.type === 'secret'}
              <!-- Never backfilled: the host sends a mask, not the value. Blank = keep. -->
              <input
                type="password"
                autocomplete="new-password"
                spellcheck="false"
                aria-labelledby={'cfg-lab-' + item.key}
                value={values[item.key] ?? ''}
                placeholder={item.secretSet ? item.secretMask || '已保存' : '未设置'}
                oninput={(e) => (values[item.key] = (e.currentTarget as HTMLInputElement).value)}
              />
            {:else}
              <input
                type={item.type === 'number' ? 'number' : 'text'}
                step={item.type === 'number' ? 'any' : undefined}
                spellcheck="false"
                aria-labelledby={'cfg-lab-' + item.key}
                value={values[item.key] ?? ''}
                placeholder={item.default ?? ''}
                oninput={(e) => (values[item.key] = (e.currentTarget as HTMLInputElement).value)}
              />
            {/if}

            {#if item.type === 'secret'}
              <span class="field-note">
                {item.secretSet ? '已保存；留空保持不变，输入新值则替换' : '未设置'}
              </span>
            {:else if item.hostWhitelist}
              <span class="field-note">这个值也决定 net.fetch 可以访问哪些域名。</span>
            {/if}
          </div>
        {/each}
      </div>

      <footer class="sheet-foot">
        <button type="button" class="chip" onclick={closeConfig}>取消</button>
        <button type="button" class="chip primary" disabled={cfgBusy || !cfg} onclick={saveConfig}>
          {cfgBusy ? '保存中…' : '保存'}
        </button>
      </footer>
    </div>
  </div>
{/if}

{#if toastText}
  <p class="toast" role="status">{toastText}</p>
{/if}

<style>
  /* One grid, two shapes: list + detail, or detail alone. Changing the columns is ALL
     「展开」 does - the frame is not remounted (a remount would destroy the plugin's
     document), and no width is taken from the rail. */
  .page {
    display: grid;
    grid-template-columns: 232px minmax(0, 1fr);
    gap: var(--space-3);
    height: 100%;
    min-height: 0;
  }
  .page.expanded {
    grid-template-columns: minmax(0, 1fr);
  }

  /* At the window's minimum width the split stops being readable: measured at 720x520, the
     detail column came out 236px wide. The list gives back 52px, and 「展开界面」 is the real
     answer below that - it is one click away and it is what the button is for. Anything
     cleverer (auto-folding, a drawer) would make the layout move on its own, which is worse
     than a cramped column the user can widen. */
  @media (max-width: 860px) {
    .page { grid-template-columns: 180px minmax(0, 1fr); }
  }

  .list {
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    padding: var(--space-2);
    overflow-y: auto;
    min-height: 0;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .list-head {
    margin: 0 0 var(--space-2);
    padding: 0 var(--space-2);
    color: var(--text-dim);
    font-size: 11px;
    display: flex;
    justify-content: space-between;
  }
  .count { font-family: var(--font-mono); }

  .row {
    appearance: none;
    border: 1px solid transparent;
    background: transparent;
    color: var(--text);
    font: inherit;
    text-align: left;
    padding: var(--space-2);
    border-radius: var(--radius-sm);
    cursor: pointer;
    display: grid;
    grid-template-columns: 8px 1fr auto;
    align-items: center;
    gap: var(--space-2);
  }
  .row:hover { background: var(--bg-sunken); }
  .row.selected { border-color: var(--accent-dim); background: var(--bg-sunken); }
  .row.is-off .row-name { color: var(--text-dim); }

  /* The state dot exists so the list can be SCANNED: which of these is off, which one has a
     broken manifest. The detail side states the same thing in words. */
  .dot {
    width: 8px;
    height: 8px;
    border-radius: 50%;
    background: var(--text-dim);
    opacity: 0.4;
  }
  .dot.on { background: var(--ok); opacity: 1; }
  .dot.err { background: var(--danger); opacity: 1; }

  .row-name {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .row-meta {
    color: var(--text-dim);
    font: var(--font-mono);
    font-size: 11px;
  }

  .detail {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    min-height: 0;
    /* Without this the column refuses to go below its content's min-content width, and the
       grid track happily overflows its own cell. Measured at 720x520 before the fix: the
       detail column was 236px while the frame inside it was 346px wide - i.e. the plugin's
       box hung 110px past the column, over the window edge. */
    min-width: 0;
  }

  .identity {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-3);
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    padding: var(--space-3) var(--space-4);
  }
  .who { min-width: 0; }
  .identity h2 {
    margin: 0;
    font-size: 15px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .meta {
    color: var(--text-dim);
    font: var(--font-mono);
    font-size: 11px;
    display: block;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .ops {
    margin-left: auto;
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }

  .switch {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-dim);
    cursor: pointer;
    font-size: 12px;
  }

  .chip {
    appearance: none;
    border: 1px solid var(--line);
    background: var(--bg-sunken);
    color: var(--text);
    font: inherit;
    font-size: 12px;
    padding: var(--space-1) var(--space-3);
    border-radius: 999px;
    cursor: pointer;
  }
  .chip:hover { border-color: var(--accent-dim); }
  .chip b { font-family: var(--font-mono); font-weight: 600; }

  /* The popover. `position: relative` on <details> makes the panel's containing block the
     summary's own box; `top: 100%` drops it under the chip and NOTHING moves. */
  .perm-drop { position: relative; }
  .perm-drop > summary { list-style: none; }
  .perm-drop > summary::-webkit-details-marker { display: none; }
  .perm-panel {
    position: absolute;
    top: calc(100% + var(--space-2));
    right: 0;
    z-index: 20;
    width: 320px;
    max-height: 60vh;
    overflow-y: auto;
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    padding: var(--space-3);
  }
  .perms {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .perm {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    color: var(--text-dim);
    font-size: 12px;
    min-width: 0;
  }
  .perm span { overflow-wrap: anywhere; }

  .frame-area {
    flex: 1 1 auto;
    min-height: 0;
    /* The iframe's default size is 300x150, and `min-width: auto` would make that its
       min-content width - so the frame's own box, not the column, decided how wide this
       could be. Same fix as .detail, one level down. */
    min-width: 0;
    display: flex;
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    /* The plugin document paints its own background edge to edge; this only clips it to the
       card's corners. Nothing here is a popover host, so clipping costs nothing. */
    overflow: hidden;
  }

  .empty-pick {
    flex: 1 1 auto;
    display: flex;
    align-items: center;
    justify-content: center;
    max-width: 46em;
    margin: 0 auto;
    text-align: center;
  }
  .no-interface { margin: 0; }

  .hint { margin: var(--space-2) 0 0; color: var(--text-dim); font-size: 12px; line-height: 1.6; }
  .err { margin: 0; color: var(--danger); font-size: 12px; }

  /* --- the settings dialog ---------------------------------------------------------- */

  /* `position: fixed` on purpose: the list and the detail are both scroll containers, and an
     absolutely positioned modal inside either of them would be clipped by it. Nothing in this
     window uses transform/filter (the sandbox token rules forbid them), so a fixed box is
     relative to the viewport, which is what a modal wants. */
  .sheet {
    position: fixed;
    inset: 0;
    z-index: 60;
    display: flex;
    align-items: center;
    justify-content: center;
    background: rgba(6, 8, 11, 0.55);
  }
  .sheet-card {
    width: min(720px, 92vw);
    max-height: 86vh;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    padding: var(--space-4);
  }
  .sheet-head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .sheet-head h3 {
    margin: 0;
    font-size: 14px;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .sheet-note { margin: 0; }
  .sheet-foot {
    display: flex;
    justify-content: flex-end;
    gap: var(--space-2);
    padding-top: var(--space-2);
    border-top: 1px solid var(--line);
  }

  /* The form scrolls, the header and the buttons do not: twelve items on a 520px-tall window
     used to push 保存 below the fold, so the user had to scroll to finish a form they had
     just filled in (docs/plugin-market.md §2.3). */
  .cfg {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(260px, 1fr));
    gap: var(--space-3) var(--space-4);
    overflow-y: auto;
    min-height: 0;
    padding: 2px;
  }
  .field {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
    min-width: 0;
  }
  .field.wide { grid-column: 1 / -1; }
  .field-label { color: var(--text-dim); font-size: 12px; }
  .field input[type='text'],
  .field input[type='number'],
  .field input[type='password'],
  .field select {
    appearance: none;
    background: var(--bg-sunken);
    border: 1px solid var(--line);
    border-radius: var(--radius-sm);
    color: var(--text);
    font: inherit;
    font-size: 12px;
    padding: var(--space-1) var(--space-2);
    min-width: 0;
  }
  .field input[type='checkbox'] { align-self: flex-start; }
  .field-note { color: var(--text-dim); font-size: 11px; line-height: 1.5; }
  .checks { display: flex; flex-wrap: wrap; gap: var(--space-2) var(--space-3); }
  .check { display: flex; align-items: center; gap: var(--space-1); color: var(--text-dim); font-size: 12px; }

  .chip.primary { background: var(--accent); border-color: var(--accent); color: #fff; }
  .chip:disabled { opacity: 0.5; cursor: default; }

  .toast {
    position: fixed;
    right: var(--space-4);
    bottom: var(--space-4);
    margin: 0;
    z-index: 70;
    background: var(--bg-raised);
    border: 1px solid var(--line);
    border-radius: var(--radius);
    color: var(--text);
    font-size: 12px;
    padding: var(--space-2) var(--space-3);
  }
</style>
