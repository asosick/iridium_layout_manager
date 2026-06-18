// Iridium Layout Manager — client glue.
//
// Iridium's Alpine build is CSP-mode (no inline expressions), so the only way
// to wire reactivity is via a named Alpine.data component registered before
// Alpine walks the DOM. The plugin renders this script in the body BEFORE the
// x-data element AND without `defer` so it runs synchronously during HTML
// parse. That gets us in early enough to attach an `alpine:init` listener —
// Alpine fires that event right before it scans the DOM, so registering the
// data inside the listener guarantees it exists by the time x-data is
// evaluated.
//
// The fallback path covers the corner case where this script loads AFTER
// Alpine has already started (e.g. injected via htmx swap into an
// already-bootstrapped page): we register the data immediately and re-init
// the affected subtrees so they pick up the fresh registration.

(function () {
    const COMPONENT_NAME = 'layout_manager';

    const componentFactory = () => ({
        editMode: false,
        // Mirrors the first <option> by default; the form posts the bound
        // value, so this just keeps the visible select state in sync.
        selectedType: '',
        // Which page (layout) is currently shown. Drives the selector-button
        // highlight and is posted (via the hidden "layout" input) on Add.
        currentLayout: 0,
        // Total customizable pages — used to bound the cmd/ctrl+N hotkeys.
        layoutCount: 1,
        // Endpoint the grid is swapped from when switching pages.
        selectUrl: '',
        // Prevent Save from racing the session cookie written by reorder.
        reorderPending: false,
        // Bound document keydown handler, kept so we can detach on destroy.
        _hotkeyHandler: null,

        init() {
            const select = this.$el.querySelector('[data-lm-type-select]');
            if (select && select.options.length > 0) {
                this.selectedType = select.options[0].value;
            }

            // Read multi-page config off the root element's data-* attributes.
            const root = this.$el;
            this.selectUrl = root.dataset.lmSelectUrl || '';
            this.layoutCount = parseInt(root.dataset.lmLayoutCount || '1', 10) || 1;
            this.currentLayout = parseInt(root.dataset.lmCurrentLayout || '0', 10) || 0;

            // cmd/ctrl + 1..9 switches pages (matches the Filament original).
            this._hotkeyHandler = (e) => this.onHotkey(e);
            document.addEventListener('keydown', this._hotkeyHandler);
        },

        destroy() {
            if (this._hotkeyHandler) {
                document.removeEventListener('keydown', this._hotkeyHandler);
                this._hotkeyHandler = null;
            }
        },

        // onHotkey switches pages on (cmd|ctrl)+N. Ignored when N is out of
        // range or when there's only one page. We preventDefault so the
        // page-switch wins even though some browsers map cmd+N to tab switching.
        onHotkey(e) {
            if (!(e.metaKey || e.ctrlKey) || e.altKey || e.shiftKey) return;
            if (this.layoutCount <= 1) return;
            const n = parseInt(e.key, 10);
            if (isNaN(n) || n < 1 || n > this.layoutCount) return;
            e.preventDefault();
            this.selectLayout(n - 1);
        },

        // selectLayout swaps the grid to the requested page and updates the
        // active-button highlight. The server re-renders the grid (and OOB-
        // refreshes the selector strip) for that layout, keeping its per-block
        // mutation URLs correct.
        selectLayout(i) {
            this.currentLayout = i;
            if (!this.selectUrl || !window.htmx) return;
            const grid = this.$root.querySelector('[data-lm-grid]');
            if (!grid) return;
            try {
                window.htmx.ajax('GET', this.selectUrl + '?layout=' + i, {
                    target: grid,
                    swap: 'outerHTML',
                });
            } catch (err) {
                console.error('[layoutmgr] selectLayout failed', err);
            }
        },

        toggleEdit() {
            this.editMode = !this.editMode;
        },

        // Alpine sort provides the moved block ID and its new zero-based
        // position. Rebuilding from both values is safe whether its callback
        // runs just before or just after the browser moves the DOM element.
        handleReorder(movedID, newPosition) {
            if (this.reorderPending) return;

            const grid = this.$el.querySelector('[data-lm-grid]');
            if (!grid) return;
            const currentOrder = Array.from(grid.querySelectorAll('[data-lm-block]'))
                .map(el => el.dataset.id)
                .filter(Boolean);
            const order = currentOrder.filter(id => id !== movedID);
            const position = Math.max(0, Math.min(Number(newPosition), order.length));
            order.splice(position, 0, movedID);
            const url = grid.dataset.reorderUrl;
            if (!url || !movedID || !Number.isInteger(Number(newPosition))) return;

            this.reorderPending = true;

            // Iridium enforces CSRF on every mutating request: a POST without a
            // valid X-CSRF-Token is rejected with 403. htmx-driven mutations
            // (add/remove/resize) get the token injected automatically via the
            // body's :hx-headers binding, but this is a RAW fetch — htmx isn't
            // involved — so we must read the current token off the Alpine
            // 'security' store and send it ourselves. Without this the reorder
            // POST is silently rejected: Alpine's sort has already moved the DOM
            // visually, so the new order LOOKS applied, but nothing persists and
            // revisiting the page shows the old order. That was the "layout not
            // placed back in the same spots" bug.
            const security = (window.Alpine && typeof window.Alpine.store === 'function')
                ? window.Alpine.store('security')
                : null;
            const headers = {
                'Content-Type': 'application/json',
                'HX-Request': 'true',
            };
            if (security && security.csrf) {
                headers['X-CSRF-Token'] = security.csrf;
            }

            fetch(url, {
                method: 'POST',
                headers,
                credentials: 'same-origin',
                body: JSON.stringify({ order }),
            })
                .then(r => {
                    // Iridium ROTATES the CSRF token on every successful mutating
                    // request and returns the fresh one in the response header.
                    // htmx normally feeds that back into the security store (via
                    // its htmx:afterRequest listener), but a raw fetch bypasses
                    // that — so we update the store ourselves. Skipping this would
                    // leave the store holding a now-stale token and 403 the NEXT
                    // htmx POST (add/remove/resize) on the page.
                    const t = r.headers.get('X-CSRF-Token');
                    if (t && security) security.csrf = t;
                    if (!r.ok) {
                        throw new Error('[layoutmgr] reorder rejected: ' + r.status);
                    }
                    return r.text();
                })
                .then(html => {
                    // Replace the grid with the server's freshly-rendered
                    // copy so order, cols, and any newly-resolved widgets
                    // come from the source of truth.
                    grid.outerHTML = html;

                    // CRITICAL: after a raw outerHTML swap, htmx and Alpine
                    // don't automatically know about the new DOM. Without
                    // re-processing, the freshly-rendered ×/+/− buttons
                    // don't get bound to htmx — so the FIRST click hits the
                    // stale (pre-reorder) button still wired up, which is
                    // why resizing widget A after a swap appeared to affect
                    // widget B for one click. We rescan both subsystems
                    // against the new grid so all the hx-post URLs the
                    // server just rendered fire correctly.
                    const newGrid = document.querySelector('[data-lm-grid]');
                    if (!newGrid) return;
                    if (window.htmx && typeof window.htmx.process === 'function') {
                        try { window.htmx.process(newGrid); } catch (e) {
                            console.error('[layoutmgr] htmx.process failed', e);
                        }
                    }
                    if (window.Alpine && typeof window.Alpine.initTree === 'function') {
                        try { window.Alpine.initTree(newGrid); } catch (e) {
                            // Re-initing a tree already initialised by Alpine
                            // when x-data was processed via the parent is
                            // usually harmless; log and continue.
                            console.warn('[layoutmgr] Alpine.initTree warn', e);
                        }
                    }
                })
                .catch(err => console.error('[layoutmgr] reorder failed', err))
                .finally(() => {
                    this.reorderPending = false;
                });
        },
    });


    const REGISTERED_FLAG = '__lmRegistered_' + COMPONENT_NAME;

    const register = () => {
        if (!window.Alpine) return false;
        window.Alpine.data(COMPONENT_NAME, componentFactory);
        window[REGISTERED_FLAG] = true;
        return true;
    };

    // Preferred path: Alpine hasn't started yet. The alpine:init event fires
    // immediately before Alpine walks the DOM, after this script has hooked
    // it. Registering inside the listener guarantees the data component is
    // available when x-data is first evaluated.
    document.addEventListener('alpine:init', register);


    if (window.Alpine && window.Alpine.version) {
        const wasRegistered = !!window[REGISTERED_FLAG];
        if (register() && !wasRegistered) {
            document.querySelectorAll('[x-data="' + COMPONENT_NAME + '"]').forEach(el => {
                // Skip anything Alpine already initialised to avoid a double-init.
                if (el._x_dataStack) return;
                try { window.Alpine.initTree(el); } catch (e) {
                    console.error('[layoutmgr] cold init failed', e);
                }
            });
        }
    }
})();
