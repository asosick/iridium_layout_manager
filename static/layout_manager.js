// Iridium Layout Manager — client glue.
//
// Two Alpine components (registered with Alpine.data, as Iridium's Alpine
// build is CSP-mode):
//
//   layout_manager        the page: modes, pages, the toolbar, the block
//                         gallery, the order of the blocks and the one queue
//                         every request goes through, so each edit reaches
//                         the server after the one before it has landed.
//   layout_manager_block  one block: its span, its controls, drag-to-resize
//                         and the masonry row span its content needs. It sits
//                         inside the page component, so it reaches the page's
//                         state and methods through Alpine's scope.
//
// Edits change the page in place: the server answers 204 (or, for Add, the
// one new block), and the other blocks keep their state. Only switching page,
// entering edit mode with unsaved changes, and Reset load a whole grid.
//
// The block gallery is a sheet at the bottom of the window with a live,
// scaled-down preview of every block on offer. A card dragged onto the grid
// places its block where it is dropped; a card clicked adds its block after
// the last.
//
// Iridium loads this plugin asset before Alpine starts, which also makes the
// components available to later HTMX page fragments.

(function () {
    // Keys typed into these never switch page.
    const EDITABLE = 'input, textarea, select, [contenteditable=""], [contenteditable="true"]';

    // Gallery previews. A block lays out as wide as a placed block (one
    // column, within these bounds), then is scaled down whole to fit its
    // card's height, but never below a scale that still reads: a taller block
    // is cut off with a fade instead. A card is as wide as its scaled block.
    const PREVIEW_MIN_WIDTH = 240;
    const PREVIEW_MAX_WIDTH = 560;
    const PREVIEW_MAX_SCALE = 0.62;
    const PREVIEW_MIN_SCALE = 0.3;
    const CARD_MIN_WIDTH = 150;
    const CARD_MAX_WIDTH = 340;
    // How many previews load at once.
    const PREVIEW_CONCURRENCY = 4;
    // The longest a preview waits, once it has landed, for its block to finish
    // drawing itself before it is shown anyway (ms).
    const PREVIEW_SETTLE_MAX = 1000;
    // A press on a card that moves this far (px) is a drag, not a click.
    const DRAG_THRESHOLD = 6;
    // Near the top or bottom of the page a drag scrolls it, faster nearer the
    // edge.
    const AUTOSCROLL_ZONE = 72;
    const AUTOSCROLL_MAX_SPEED = 22;

    // Iridium's animation setting, or the system's, turns scripted motion off.
    const motionOff = () => document.documentElement.dataset.iridiumAnimationMode === 'none'
        || window.matchMedia('(prefers-reduced-motion: reduce)').matches;

    // The space between a grid's rows, from its computed styles. A page set
    // to no spacing has none: 0 is a gap, not a missing one.
    const rowGapOf = (styles) => {
        const gap = parseFloat(styles.rowGap);
        return Number.isFinite(gap) ? gap : 16;
    };

    // An inert document to build copies in: nothing in it loads.
    let inert = null;

    // staticClone copies rendered markup as a picture of itself: without
    // Alpine, htmx or ids (but an SVG's, which its fills refer to), so nothing
    // in the copy starts up again, and with its canvases repainted. A video
    // becomes a still of the frame it shows, as a copy of it would load its
    // source again (a stream's blob URL fails to); audio and embedded frames
    // go. The copy is built in an inert document, so nothing loads before
    // that is done.
    const staticClone = (node) => {
        inert = inert || document.implementation.createHTMLDocument('');
        const copy = inert.importNode(node, true);
        const strip = (el) => {
            for (const attr of Array.from(el.attributes)) {
                const name = attr.name;
                if (name.startsWith('x-') || name.startsWith('@') || name.startsWith(':')
                    || name.startsWith('hx-') || name.startsWith('data-lm-')
                    || (name === 'id' && !el.closest('svg'))) {
                    el.removeAttribute(name);
                }
            }
        };
        strip(copy);
        copy.querySelectorAll('*').forEach(strip);
        const sourceCanvases = node.querySelectorAll('canvas');
        const paint = Array.from(copy.querySelectorAll('canvas'), (canvas, i) => [canvas, sourceCanvases[i]]);
        const sourceVideos = node.querySelectorAll('video');
        copy.querySelectorAll('video').forEach((video, i) => {
            const from = sourceVideos[i];
            const still = inert.createElement('canvas');
            ['class', 'style', 'hidden'].forEach(name => {
                if (video.hasAttribute(name)) still.setAttribute(name, video.getAttribute(name));
            });
            still.width = (from && from.videoWidth) || 16;
            still.height = (from && from.videoHeight) || 9;
            // The box the video is laid out in, whatever styled it.
            if (from && from.clientWidth) {
                still.style.width = from.clientWidth + 'px';
                still.style.height = from.clientHeight + 'px';
            }
            paint.push([still, from]);
            video.replaceWith(still);
        });
        copy.querySelectorAll('audio, iframe, object, embed').forEach(el => el.remove());
        const clone = document.adoptNode(copy);
        paint.forEach(([to, from]) => {
            try {
                if (from) to.getContext('2d').drawImage(from, 0, 0, to.width, to.height);
            } catch (err) { /* nothing drawn yet, or tainted: left blank */ }
        });
        clone.setAttribute('x-ignore', '');
        clone.setAttribute('inert', '');
        clone.setAttribute('aria-hidden', 'true');
        return clone;
    };

    // scrollParent is the element that scrolls el: Iridium's main area, or
    // the document.
    const scrollParent = (el) => {
        for (let node = el && el.parentElement; node && node !== document.body; node = node.parentElement) {
            const overflow = window.getComputedStyle(node).overflowY;
            if (overflow === 'auto' || overflow === 'scroll') return node;
        }
        return document.scrollingElement || document.documentElement;
    };

    // markWords writes text into el with every one of words (lower case)
    // marked.
    const markWords = (el, text, words) => {
        if (!el) return;
        const lower = text.toLowerCase();
        const marked = new Array(text.length).fill(false);
        // Lower-casing can change a string's length; then nothing is marked.
        if (lower.length === text.length) {
            words.forEach(word => {
                for (let i = lower.indexOf(word); i >= 0; i = lower.indexOf(word, i + word.length)) {
                    marked.fill(true, i, i + word.length);
                }
            });
        }
        el.textContent = '';
        for (let i = 0; i < text.length;) {
            let j = i;
            while (j < text.length && marked[j] === marked[i]) j++;
            const part = text.slice(i, j);
            if (marked[i]) {
                const mark = document.createElement('mark');
                mark.textContent = part;
                el.appendChild(mark);
            } else {
                el.appendChild(document.createTextNode(part));
            }
            i = j;
        }
    };

    const layoutManager = () => ({
        editMode: false,
        // Which page (layout) is shown: the selector highlight and every
        // request's layout.
        currentLayout: 0,
        // The pages, each one's name ('' shows its number), and which hold
        // blocks, from the grid shown (data-lm-names, data-lm-populated).
        pages: [],
        populated: [],
        // Whether the user adds and removes pages (no fixed LayoutCount), and
        // names them.
        addablePages: false,
        renamablePages: false,
        // The page being renamed (its index, else -1) and the name typed.
        renaming: -1,
        renameValue: '',
        // The block IDs in the order they show.
        order: [],
        lockAfterDone: true,
        zenMode: false,
        // The block gallery: whether it is open, the search typed, how many
        // cards match it of how many, and whether a card is being dragged
        // (the sheet steps aside) with the pointer back over the sheet (a
        // release there puts nothing down).
        galleryOpen: false,
        gallerySearch: '',
        galleryShown: 0,
        galleryTotal: 0,
        galleryDragging: false,
        galleryOverSheet: false,
        // The page settings: the shown page's columns and chosen spacing
        // ('' follows the vertical padding), from its grid (data-lm-columns,
        // data-lm-spacing); whether the dialog is open, and the choices in it.
        pageColumns: 0,
        pageSpacing: '',
        settingsOpen: false,
        settingsColumns: '',
        settingsSpacing: '',
        // The page root. Not this.$el: in a method an Alpine directive calls
        // (@click on a toolbar button), $el is that button.
        _root: null,
        _urls: null,
        _queue: null,
        _reorderQueued: false,
        _listeners: null,
        // The gallery's sheet (teleported to <body>), its previews' loading
        // and the card being dragged.
        _gallery: null,
        _previews: null,
        _warming: false,
        _drag: null,
        // When the last drag of a card ended, so the click that ends it adds
        // nothing more.
        _dragUpAt: 0,

        init() {
            const root = this.$el;
            this._root = root;

            const data = root.dataset;
            this._urls = {
                select: data.lmSelectUrl || '',
                add: data.lmAddUrl || '',
                save: data.lmSaveUrl || '',
                reset: data.lmResetUrl || '',
                addPage: data.lmAddPageUrl || '',
                removePage: data.lmRemovePageUrl || '',
                renamePage: data.lmRenamePageUrl || '',
                settings: data.lmSettingsUrl || '',
            };
            this.lockAfterDone = data.lmLockAfterDone !== 'false';
            this.editMode = data.lmEditDefault === 'true';
            this.addablePages = data.lmAddablePages === 'true';
            this.renamablePages = data.lmRenamablePages === 'true';
            this.currentLayout = parseInt(data.lmCurrentLayout || '0', 10) || 0;
            this._queue = Promise.resolve();

            // Kept so destroy() can detach them when Iridium's single-page
            // navigation takes the page away.
            this._listeners = {
                hotkey: (e) => this.onHotkey(e),
                zenEscape: (e) => this.onZenEscape(e),
                galleryEscape: (e) => this.onGalleryEscape(e),
                resize: () => this.galleryResized(),
                // Leaving the page: previews still loading are cancelled, so
                // the browser does not report them as failed fetches.
                leave: () => this.abortPreviews(),
                back: (e) => {
                    if (e.persisted) this.resumePreviews();
                },
                // SortableJS reports a finished drag as a 'sort' event on the
                // grid; a sortable list inside a block reports its own.
                sort: (e) => {
                    if (e.target === this.grid()) this.handleReorder();
                },
            };
            document.addEventListener('keydown', this._listeners.hotkey);
            document.addEventListener('keydown', this._listeners.galleryEscape);
            // Capture Escape before page-level shortcuts so leaving Zen mode
            // cannot trigger another action on the same key press.
            window.addEventListener('keydown', this._listeners.zenEscape, true);
            window.addEventListener('resize', this._listeners.resize);
            window.addEventListener('beforeunload', this._listeners.leave);
            window.addEventListener('pagehide', this._listeners.leave);
            window.addEventListener('pageshow', this._listeners.back);
            root.addEventListener('sort', this._listeners.sort);
            // Entering edit mode starts the gallery's first previews loading;
            // leaving it puts the gallery away.
            this.$watch('editMode', (editing) => {
                if (editing) this.warmGallery();
                else this.closeGallery();
            });
            this.gridShown();
            if (this.editMode) this.warmGallery();
        },

        destroy() {
            const l = this._listeners;
            if (!l) return;
            document.removeEventListener('keydown', l.hotkey);
            document.removeEventListener('keydown', l.galleryEscape);
            window.removeEventListener('keydown', l.zenEscape, true);
            window.removeEventListener('resize', l.resize);
            window.removeEventListener('beforeunload', l.leave);
            window.removeEventListener('pagehide', l.leave);
            window.removeEventListener('pageshow', l.back);
            this._root.removeEventListener('sort', l.sort);
            this._listeners = null;
            this.cancelDrag();
            this.abortPreviews();
            if (this._previews) {
                this._previews.observer.disconnect();
                this._previews.resize.disconnect();
                this._previews.area.disconnect();
                this._previews = null;
            }
        },

        rootClass() {
            let classes = this.editMode ? 'lm-editing' : '';
            if (this.galleryOpen) classes += ' lm-gallery-open';
            return classes;
        },

        // --- the grid -----------------------------------------------------------

        grid() {
            return this._root.querySelector('[data-lm-grid]');
        },

        blockElements() {
            const grid = this.grid();
            return grid ? Array.from(grid.querySelectorAll(':scope > [data-lm-block]')) : [];
        },

        // A grid was rendered: read its pages and order, then switch masonry
        // on once its blocks have measured themselves.
        gridShown() {
            const grid = this.grid();
            if (!grid) return;
            this.populated = (grid.dataset.lmPopulated || '').split(',').map(flag => flag === '1');
            let names = null;
            try { names = JSON.parse(grid.dataset.lmNames || 'null'); } catch (err) { /* numbers below */ }
            this.pages = Array.isArray(names) ? names : this.populated.map(() => '');
            this.pageColumns = parseInt(grid.dataset.lmColumns || '', 10) || 1;
            this.pageSpacing = grid.dataset.lmSpacing || '';
            // The page shown can differ from the one asked for: a page added
            // or removed, or one the committed layout does not have.
            const shown = parseInt(grid.dataset.lmShown || '', 10);
            if (Number.isInteger(shown) && shown !== this.currentLayout) this.keepInAddress(shown);
            this.syncOrder();
            window.requestAnimationFrame(() => grid.classList.add('lm-masonry-ready'));
        },

        // Blocks were added, removed or moved in the grid shown.
        syncOrder() {
            this.order = this.blockElements().map(el => el.dataset.id);
            if (this.currentLayout < this.populated.length) {
                this.populated[this.currentLayout] = this.order.length > 0;
            }
        },

        selectorsShown() {
            return this.editMode || this.populated.filter(Boolean).length > 1;
        },

        pageShown(i) {
            return this.editMode || !!this.populated[i];
        },

        pageLabel(i) {
            return this.pages[i] || this.pageNumber(i);
        },

        pageNumber(i) {
            return String(i + 1);
        },

        pageClass(i) {
            return this.currentLayout === i ? 'ir-btn-primary' : 'ir-btn-outline';
        },

        isRenaming(i) {
            return this.renaming === i;
        },

        renameFieldClass(i) {
            return this.renaming === i ? '' : 'lm-page-name-hidden';
        },

        canRemovePage() {
            return this.editMode && this.addablePages && this.pages.length > 1;
        },

        // --- requests -------------------------------------------------------------

        // enqueue runs task once every earlier task has finished.
        enqueue(task) {
            const run = this._queue.then(task).catch(err => {
                console.error('[layoutmgr]', err);
            });
            this._queue = run;
            return run;
        },

        // post sends an edit. A refused one is reported as Iridium reports a
        // failed htmx request: an error toast with the server's message.
        async post(url, body) {
            const security = window.Alpine.store('security');
            const headers = { 'HX-Request': 'true' };
            if (security && security.csrf) headers['X-CSRF-Token'] = security.csrf;
            if (body) headers['Content-Type'] = 'application/x-www-form-urlencoded';
            let response;
            try {
                response = await fetch(url, { method: 'POST', headers, credentials: 'same-origin', body });
            } catch (err) {
                this.notifyError('The server could not be reached.');
                return { ok: false, status: 0, headers: new Headers() };
            }
            const token = response.headers.get('X-CSRF-Token');
            if (token && security) security.csrf = token;
            if (!response.ok) {
                let description = '';
                try { description = (await response.text()).trim(); } catch (err) { /* no body */ }
                this.notifyError(description);
                return response;
            }
            // The events the response names in HX-Trigger (Iridium's toasts).
            const trigger = response.headers.get('HX-Trigger');
            if (trigger) {
                try {
                    const events = JSON.parse(trigger);
                    Object.keys(events).forEach(name => {
                        window.dispatchEvent(new CustomEvent(name, { detail: events[name] }));
                    });
                } catch (err) { /* not JSON: no events */ }
            }
            return response;
        },

        notifyError(description) {
            window.dispatchEvent(new CustomEvent('notify', {
                detail: { type: 'error', message: 'Request failed', description },
            }));
        },

        // swapGrid replaces the grid through htmx, which gives the new
        // widgets' htmx controls this page's nonce and initialises them.
        async swapGrid(method, url) {
            const grid = this.grid();
            if (!grid) return;
            await window.htmx.ajax(method, url, { source: grid, target: grid, swap: 'outerHTML' });
            this.gridShown();
        },

        loadPage(i) {
            return this.swapGrid('GET', this._urls.select + '?layout=' + i + '&mode=' + (this.editMode ? 'edit' : 'view'));
        },

        // insertBlock asks the server for a new block of type key at index at
        // (-1: after the last) and puts it in place of target, or at the end
        // of the grid. It runs in the queue, and answers the new block.
        async insertBlock(key, at, target) {
            const grid = this.grid();
            if (!grid) return null;
            const before = new Set(this.order);
            let url = this._urls.add + '?type=' + encodeURIComponent(key) + '&layout=' + this.currentLayout;
            if (at >= 0) url += '&at=' + at;
            try {
                if (target) await window.htmx.ajax('POST', url, { source: grid, target, swap: 'outerHTML' });
                else await window.htmx.ajax('POST', url, { source: grid, target: grid, swap: 'beforeend' });
            } finally {
                // Refused: the slot goes.
                if (target && target.isConnected) target.remove();
            }
            this.syncOrder();
            const id = this.order.find(candidate => !before.has(candidate));
            const block = id ? this.blockElements().find(el => el.dataset.id === id) : null;
            if (block) this.pulse(block, 'lm-block-arrived', 1600);
            return block || null;
        },

        // --- toolbar --------------------------------------------------------------

        save() {
            this.enqueue(async () => {
                const response = await this.post(this._urls.save, 'layout=' + this.currentLayout);
                if (!response.ok) return;
                const grid = this.grid();
                if (grid) grid.dataset.lmUnsaved = 'false';
                if (this.lockAfterDone) this.editMode = false;
                const next = parseInt(response.headers.get('X-Iridium-Layout-Manager-Next-Layout') || '', 10);
                if (Number.isInteger(next) && next >= 0 && next < this.pages.length && next !== this.currentLayout) {
                    this.selectLayout(next);
                }
            });
        },

        // reset discards the draft and keeps editing; cancel also leaves edit
        // mode, back to the committed layout.
        reset() {
            this.enqueue(() => this.swapGrid('POST', this._urls.reset + '?layout=' + this.currentLayout));
        },

        cancel() {
            this.enqueue(async () => {
                await this.swapGrid('POST', this._urls.reset + '?layout=' + this.currentLayout);
                this.editMode = false;
            });
        },

        startEditing() {
            this.editMode = true;
            // View mode shows the committed layout: load the draft if it differs.
            const grid = this.grid();
            if (grid && grid.dataset.lmUnsaved === 'true') {
                this.enqueue(() => this.loadPage(this.currentLayout));
            }
        },

        // selectLayout shows page i.
        selectLayout(i) {
            this.keepInAddress(i);
            this.enqueue(() => this.loadPage(i));
        },

        // keepInAddress makes page i the current one and keeps it in the
        // address, so a reload stays on it.
        keepInAddress(i) {
            this.currentLayout = i;
            const url = new URL(window.location.href);
            url.searchParams.set('layout', String(i));
            window.history.replaceState(window.history.state, '', url);
        },

        // 1..9 switches page, unless the key is typed into a field or comes
        // with a modifier: Ctrl/Cmd+digit belongs to the browser's tabs. Not
        // behind the settings dialog either.
        onHotkey(e) {
            if (e.defaultPrevented || e.isComposing || e.metaKey || e.ctrlKey || e.altKey) return;
            if (this.settingsOpen) return;
            if (this.pages.length <= 1 || !/^[1-9]$/.test(e.key)) return;
            if (e.target && e.target.closest && e.target.closest(EDITABLE)) return;
            const n = parseInt(e.key, 10);
            if (n > this.pages.length) return;
            e.preventDefault();
            this.selectLayout(n - 1);
        },

        // --- pages ------------------------------------------------------------------
        //
        // Adding, removing and renaming pages are edits like any other: they
        // change the draft, which Done commits and Reset or Cancel discards.

        // addPage adds an empty page after the last and shows it.
        addPage() {
            if (!this.addablePages) return;
            this.enqueue(() => this.swapGrid('POST', this._urls.addPage));
        },

        // removePage removes the page shown, with its blocks (asking first if
        // it has any), and shows the one before it.
        removePage() {
            if (!this.canRemovePage()) return;
            const layout = this.currentLayout;
            if (this.order.length > 0
                && !window.confirm('Remove page "' + this.pageLabel(layout) + '" and its blocks?')) return;
            this.enqueue(() => this.swapGrid('POST', this._urls.removePage + '?layout=' + layout));
        },

        // startRename shows a field in place of page i's button.
        startRename(i) {
            if (!this.editMode || !this.renamablePages || i < 0 || i >= this.pages.length) return;
            if (i !== this.currentLayout) this.selectLayout(i);
            this.renaming = i;
            this.renameValue = this.pages[i] || '';
            this.$nextTick(() => {
                const field = this._root.querySelectorAll('#lm-selectors .lm-page-name')[i];
                if (field) {
                    field.focus();
                    field.select();
                }
            });
        },

        // commitRename keeps the name typed (Enter, or leaving the field); an
        // empty name shows the page's number again.
        commitRename() {
            const i = this.renaming;
            if (i < 0) return;
            this.renaming = -1;
            const name = (this.renameValue || '').trim();
            if (name === (this.pages[i] || '')) return;
            this.enqueue(async () => {
                const body = new URLSearchParams({ name }).toString();
                const response = await this.post(this._urls.renamePage + '?layout=' + i, body);
                if (response.ok) this.pages[i] = await response.text();
            });
        },

        cancelRename() {
            this.renaming = -1;
        },

        enterZen() {
            this.zenMode = true;
        },

        exitZen() {
            this.zenMode = false;
        },

        onZenEscape(e) {
            if (!this.zenMode || e.key !== 'Escape') return;
            e.preventDefault();
            e.stopPropagation();
            this.exitZen();
        },

        // --- page settings --------------------------------------------------------
        //
        // The dialog chooses the shown page's columns and the space between
        // its blocks. Apply is an edit like any other: Done keeps it, Reset or
        // Cancel undoes it.

        settingsDialog() {
            return this._root.querySelector('[data-lm-settings]');
        },

        // defaultSpacing is the spacing the page's vertical padding gives its
        // blocks, which a page has until the user chooses another.
        defaultSpacing() {
            const padding = this._root.dataset.verticalPadding;
            if (padding === 'compact') return 'compact';
            if (padding === 'none') return 'tight';
            return 'normal';
        },

        shownSpacing() {
            return this.pageSpacing || this.defaultSpacing();
        },

        openSettings() {
            const dialog = this.settingsDialog();
            if (!dialog || !this.editMode || dialog.open) return;
            this.closeGallery();
            this.settingsColumns = String(this.pageColumns);
            this.settingsSpacing = this.shownSpacing();
            this.settingsOpen = true;
            dialog.showModal();
            // The columns chosen has the focus, so the arrow keys change them.
            this.$nextTick(() => {
                const checked = dialog.querySelector('input[name="lm-columns"]:checked');
                if (checked) checked.focus();
            });
        },

        closeSettings() {
            const dialog = this.settingsDialog();
            if (dialog && dialog.open) dialog.close();
        },

        // The dialog closed: Escape, Cancel, its close button, a click beside
        // it, or Apply.
        settingsClosed() {
            this.settingsOpen = false;
        },

        settingsChanged() {
            return this.settingsColumns !== String(this.pageColumns)
                || this.settingsSpacing !== this.shownSpacing();
        },

        settingsDescription() {
            if (this.pages.length <= 1) return 'How this page lays out its blocks.';
            const name = this.pages[this.currentLayout];
            return name
                ? 'How “' + name + '” lays out its blocks.'
                : 'How page ' + this.pageNumber(this.currentLayout) + ' lays out its blocks.';
        },

        // applySettings sends the choices, then shows the page's grid as the
        // server laid it out. The spacing the page would have anyway is sent
        // as none chosen, so the page keeps following its vertical padding.
        applySettings() {
            if (!this.settingsChanged() || !this._urls.settings) {
                this.closeSettings();
                return;
            }
            const params = new URLSearchParams({
                layout: String(this.currentLayout),
                columns: this.settingsColumns,
                spacing: this.settingsSpacing === this.defaultSpacing() ? '' : this.settingsSpacing,
            });
            this.closeSettings();
            this.enqueue(() => this.swapGrid('POST', this._urls.settings + '?' + params.toString()));
        },

        // --- order ----------------------------------------------------------------

        // A finished drag: persist the order the grid now shows. A drag made
        // while a reorder is on its way is sent after it, with the order the
        // grid shows then.
        handleReorder() {
            this.syncOrder();
            if (this._reorderQueued) return;
            this._reorderQueued = true;
            this.enqueue(async () => {
                this._reorderQueued = false;
                const grid = this.grid();
                if (!grid || this.order.length === 0) return;
                const body = new URLSearchParams(this.order.map(id => ['order', id])).toString();
                const response = await this.post(grid.dataset.reorderUrl, body);
                // Refused: show the order the server kept.
                if (!response.ok) await this.loadPage(this.currentLayout);
            });
        },

        // --- the block gallery --------------------------------------------------------

        // gallery is the sheet, which x-teleport moved to <body>.
        gallery() {
            if (this._gallery && this._gallery.isConnected) return this._gallery;
            const template = this._root.querySelector('[data-lm-gallery-template]');
            this._gallery = (template && template._x_teleport) || null;
            return this._gallery;
        },

        galleryRow() {
            const sheet = this.gallery();
            return sheet ? sheet.querySelector('[data-lm-gallery-row]') : null;
        },

        galleryCards() {
            const sheet = this.gallery();
            return sheet ? Array.from(sheet.querySelectorAll('[data-lm-card]')) : [];
        },

        visibleCards() {
            return this.galleryCards().filter(card => !card.hidden);
        },

        galleryClass() {
            let classes = this.galleryOpen ? 'is-open' : '';
            if (this.galleryDragging) classes += ' is-dragging';
            if (this.galleryOverSheet) classes += ' is-over';
            return classes;
        },

        galleryToggleClass() {
            return this.galleryOpen ? 'is-active' : '';
        },

        galleryEmpty() {
            return this.galleryTotal > 0 && this.galleryShown === 0;
        },

        galleryCount() {
            if (!this.gallerySearch.trim()) return this.galleryTotal === 1 ? '1 block' : this.galleryTotal + ' blocks';
            return this.galleryShown + ' of ' + this.galleryTotal;
        },

        toggleGallery() {
            if (this.galleryOpen) this.closeGallery();
            else this.openGallery();
        },

        openGallery() {
            if (!this.editMode || this.galleryOpen || !this.gallery()) return;
            this.galleryOpen = true;
            this.placeGallery();
            this.filterGallery();
            this.$nextTick(() => {
                this.startPreviews();
                this.updateGalleryFades();
                // A pointer that comes with a keyboard goes straight to the
                // search. On a touch screen the keyboard would cover the sheet.
                if (window.matchMedia('(pointer: fine)').matches) {
                    const search = this.gallery().querySelector('[data-lm-gallery-search]');
                    if (search) search.focus({ preventScroll: true });
                }
            });
        },

        closeGallery() {
            if (!this.galleryOpen) return;
            this.cancelDrag();
            const sheet = this.gallery();
            const hadFocus = !!sheet && sheet.contains(document.activeElement);
            this.galleryOpen = false;
            if (hadFocus) {
                const toggle = this._root.querySelector('[data-lm-gallery-toggle]');
                if (toggle && toggle.offsetParent !== null) toggle.focus({ preventScroll: true });
                else document.activeElement.blur();
            }
        },

        // placeGallery lines the sheet up with the grid, and leaves room below
        // the grid to scroll its last blocks up above the sheet.
        placeGallery() {
            const sheet = this.gallery();
            const wrap = this._root.querySelector('.lm-grid-wrap');
            if (!sheet || !wrap) return;
            const viewport = document.documentElement.clientWidth;
            const rect = wrap.getBoundingClientRect();
            let left = 0;
            let right = 0;
            if (viewport >= 640) {
                left = Math.max(8, Math.round(rect.left) - 16);
                right = Math.max(8, Math.round(viewport - rect.right) - 16);
            }
            sheet.style.setProperty('--lm-gallery-left', left + 'px');
            sheet.style.setProperty('--lm-gallery-right', right + 'px');
            this._root.style.setProperty('--lm-gallery-space', sheet.offsetHeight + 'px');
        },

        // The window or the grid changed size: the sheet follows the grid,
        // and the previews lay out again at a placed block's new width.
        galleryResized() {
            if (!this.galleryOpen) return;
            this.placeGallery();
            const previews = this._previews;
            if (previews) {
                const width = this.previewWidth();
                if (width !== previews.width) {
                    previews.width = width;
                    this.galleryCards().forEach(card => {
                        const stage = card.querySelector('[data-lm-card-stage]');
                        if (stage && card.dataset.lmPreview) stage.style.width = width + 'px';
                        this.fitPreview(card);
                    });
                }
            }
            this.updateGalleryFades();
        },

        // previewWidth is the width a block lays out at in a preview: a placed
        // block's, one column of the grid.
        previewWidth() {
            const grid = this.grid();
            if (!grid) return 320;
            const styles = window.getComputedStyle(grid);
            const columns = Math.max(1, styles.gridTemplateColumns.split(' ').length);
            const gap = parseFloat(styles.columnGap) || 0;
            const width = (grid.clientWidth - gap * (columns - 1)) / columns;
            return Math.round(Math.min(PREVIEW_MAX_WIDTH, Math.max(PREVIEW_MIN_WIDTH, width || 320)));
        },

        // warmGallery starts loading the previews the gallery shows first, in
        // the browser's idle time once the page is being edited, so they are
        // drawn by the time the gallery opens rather than after.
        warmGallery() {
            if (this._warming || this._previews) return;
            this._warming = true;
            const warm = () => {
                this._warming = false;
                if (!this.editMode || !this.gallery()) return;
                this.placeGallery();
                this.startPreviews();
                this.queueFirstPreviews();
            };
            if (window.requestIdleCallback) window.requestIdleCallback(warm, { timeout: 800 });
            else window.setTimeout(warm, 200);
        },

        // queueFirstPreviews loads the previews of the cards the row shows
        // first, and a little past them, without waiting for them to scroll
        // into view (the sheet may not be open yet).
        queueFirstPreviews() {
            const row = this.galleryRow();
            if (!row || !this._previews) return;
            const reach = row.clientWidth + 320;
            let x = 0;
            for (const card of this.visibleCards()) {
                if (x > reach) break;
                this.queuePreview(card);
                x += card.offsetWidth + 14;
            }
        },

        // startPreviews loads each card's preview the first time it scrolls
        // into (or near) view, and refits a preview whose block changes size.
        startPreviews() {
            if (this._previews) return;
            const row = this.galleryRow();
            if (!row) return;
            const previews = {
                width: this.previewWidth(),
                queue: [],
                active: 0,
                // Each loading card's request abort, and whether the page is
                // being left (see abortPreviews).
                aborts: new Map(),
                leaving: false,
                observer: new IntersectionObserver((entries) => {
                    entries.forEach(entry => {
                        if (entry.isIntersecting) this.queuePreview(entry.target);
                    });
                }, { root: row, rootMargin: '0px 320px' }),
                resize: new ResizeObserver((entries) => {
                    entries.forEach(entry => {
                        const card = entry.target.closest('[data-lm-card]');
                        if (card) this.fitPreview(card);
                    });
                }),
                // The area the page is drawn in, whose size changes with the
                // window and the sidebar but never with the blocks (watching
                // anything that grows with them would see the masonry's own
                // changes as a ResizeObserver loop). A frame later, as
                // placing the sheet changes the page's padding.
                area: new ResizeObserver(() => window.requestAnimationFrame(() => this.galleryResized())),
            };
            this._previews = previews;
            this.galleryCards().forEach(card => {
                this.fitPreview(card);
                previews.observer.observe(card);
            });
            const area = this._root.closest('[data-main]');
            if (area) previews.area.observe(area);
        },

        queuePreview(card) {
            const previews = this._previews;
            if (!previews || previews.leaving || card.dataset.lmPreview) return;
            card.dataset.lmPreview = 'queued';
            previews.queue.push(card);
            this.pumpPreviews();
        },

        pumpPreviews() {
            const previews = this._previews;
            while (previews && previews.active < PREVIEW_CONCURRENCY && previews.queue.length > 0) {
                const card = previews.queue.shift();
                previews.active++;
                this.loadPreview(card).finally(() => {
                    previews.active--;
                    if (this._previews === previews) this.pumpPreviews();
                });
            }
        },

        // loadPreview has the server render the card's block, laid out as wide
        // as a placed block, through htmx: the block's own scripts start as
        // they do on the page (a chart draws itself, a table loads). The
        // block stays hidden behind the card's placeholder until it has
        // finished drawing, then fades in, so a half-drawn block never shows.
        async loadPreview(card) {
            const stage = card.querySelector('[data-lm-card-stage]');
            const previews = this._previews;
            if (!stage || !card.isConnected || !previews) return;
            card.dataset.lmPreview = 'loading';
            stage.style.width = previews.width + 'px';
            // htmx hands its request to htmx:before:request: keep its abort,
            // and have a fetch cancelled as the page is left answer nothing
            // (204, which htmx does not swap) rather than fail, which htmx
            // and the browser would report as an error.
            const capture = (e) => {
                const ctx = e.detail && e.detail.ctx;
                if (!ctx || !ctx.request) return;
                if (ctx.request.abort) previews.aborts.set(card, ctx.request.abort);
                const send = ctx.fetch || window.fetch.bind(window);
                ctx.fetch = (url, init) => send(url, init).catch((err) => {
                    if (previews.leaving) return new Response(null, { status: 204 });
                    throw err;
                });
            };
            stage.addEventListener('htmx:before:request', capture);
            try {
                await window.htmx.ajax('GET', card.dataset.previewUrl, { source: stage, target: stage, swap: 'innerHTML' });
            } catch (err) {
                if (!previews.leaving) console.warn('[layoutmgr] preview of', card.dataset.key, err);
            } finally {
                stage.removeEventListener('htmx:before:request', capture);
                previews.aborts.delete(card);
            }
            // Cancelled as the page was left: it loads again if the page
            // comes back (from the back/forward cache).
            if (previews.leaving && stage.childElementCount === 0) {
                delete card.dataset.lmPreview;
                return;
            }
            if (stage.childElementCount > 0) await this.previewSettled(stage);
            card.dataset.lmPreview = stage.childElementCount > 0 ? 'ready' : 'failed';
            this.fitPreview(card);
            if (this._previews) this._previews.resize.observe(stage);
        },

        // abortPreviews cancels the previews loading and waiting to: the page
        // is being left.
        abortPreviews() {
            const previews = this._previews;
            if (!previews) return;
            previews.leaving = true;
            previews.queue.forEach(card => { delete card.dataset.lmPreview; });
            previews.queue = [];
            previews.aborts.forEach(abort => {
                try { abort(); } catch (err) { /* already over */ }
            });
            previews.aborts.clear();
        },

        // resumePreviews loads the previews again after the page came back
        // from the back/forward cache with its loading cancelled.
        resumePreviews() {
            const previews = this._previews;
            if (!previews || !previews.leaving) return;
            previews.leaving = false;
            if (this.editMode) this.queueFirstPreviews();
        },

        // previewSettled waits, up to PREVIEW_SETTLE_MAX, for a block that
        // just landed to finish drawing: its charts started (Iridium marks a
        // chart waiting to start data-apex-chart-pending) and its height
        // still for two checks in a row.
        previewSettled(stage) {
            return new Promise((resolve) => {
                const started = performance.now();
                let last = -1;
                let still = 0;
                const check = () => {
                    const height = stage.offsetHeight;
                    const pending = stage.querySelector('[data-apex-chart-pending]');
                    still = !pending && height === last ? still + 1 : 0;
                    last = height;
                    if (still >= 2 || performance.now() - started > PREVIEW_SETTLE_MAX) resolve();
                    else window.setTimeout(check, 80);
                };
                window.setTimeout(check, 80);
            });
        },

        // fitPreview scales the card's block down to the card's height, within
        // PREVIEW_MIN_SCALE..PREVIEW_MAX_SCALE, and makes the card as wide as
        // the block it shows.
        fitPreview(card) {
            const preview = card.querySelector('[data-lm-card-preview]');
            const stage = card.querySelector('[data-lm-card-stage]');
            if (!preview || !stage || !this._previews) return;
            const width = this._previews.width;
            const height = preview.clientHeight;
            const natural = card.dataset.lmPreview === 'ready' ? Math.max(1, stage.offsetHeight) : 0;
            let scale = Math.min(PREVIEW_MAX_SCALE, CARD_MAX_WIDTH / width);
            if (natural > 0) scale = Math.min(scale, Math.max(PREVIEW_MIN_SCALE, height / natural));
            const cardWidth = Math.round(Math.max(CARD_MIN_WIDTH, Math.min(CARD_MAX_WIDTH, width * scale)));
            const x = Math.max(0, Math.round((cardWidth - width * scale) / 2));
            const y = natural > 0 ? Math.max(0, Math.round((height - natural * scale) / 2)) : 0;
            card.style.width = cardWidth + 'px';
            stage.style.transform = 'translate(' + x + 'px, ' + y + 'px) scale(' + scale.toFixed(4) + ')';
            card.classList.toggle('lm-card-clipped', natural * scale > height + 1);
        },

        onGalleryScroll() {
            this.updateGalleryFades();
        },

        // The row's ends fade out while there is more to scroll to.
        updateGalleryFades() {
            const row = this.galleryRow();
            if (!row) return;
            const end = row.scrollWidth - row.clientWidth;
            row.classList.toggle('lm-fade-start', row.scrollLeft > 4);
            row.classList.toggle('lm-fade-end', row.scrollLeft < end - 4);
        },

        // A mouse wheel scrolls the row sideways; a trackpad already does.
        onGalleryWheel(e) {
            const row = e.currentTarget;
            if (e.ctrlKey || Math.abs(e.deltaX) >= Math.abs(e.deltaY)) return;
            if (row.scrollWidth <= row.clientWidth) return;
            e.preventDefault();
            const unit = e.deltaMode === 1 ? 40 : e.deltaMode === 2 ? row.clientWidth : 1;
            row.scrollLeft += e.deltaY * unit;
        },

        // filterGallery shows the cards whose name (or key) holds every word
        // typed, and marks the words in their names.
        filterGallery() {
            const words = (this.gallerySearch || '').toLowerCase().split(/\s+/).filter(Boolean);
            const cards = this.galleryCards();
            let shown = 0;
            cards.forEach(card => {
                const label = card.dataset.label || '';
                const haystack = (label + ' ' + (card.dataset.key || '')).toLowerCase();
                const match = words.every(word => haystack.includes(word));
                card.hidden = !match;
                // Its place in the entrance as the sheet opens.
                if (match) card.style.setProperty('--lm-i', String(Math.min(shown, 10)));
                if (match) shown++;
                markWords(card.querySelector('[data-lm-card-label]'), label, words);
            });
            this.galleryShown = shown;
            this.galleryTotal = cards.length;
            const row = this.galleryRow();
            if (row) row.scrollLeft = 0;
            this.updateGalleryFades();
        },

        // In the search: Enter adds the first match, Down goes to the cards,
        // Escape clears what was typed (then, with nothing typed, closes).
        onSearchKeydown(e) {
            if (e.key === 'Enter') {
                e.preventDefault();
                const card = this.visibleCards()[0];
                if (card) this.addCard(card);
            } else if (e.key === 'ArrowDown') {
                e.preventDefault();
                this.focusCard(this.visibleCards()[0]);
            } else if (e.key === 'Escape' && this.gallerySearch) {
                e.preventDefault();
                this.gallerySearch = '';
                this.filterGallery();
            }
        },

        // On a card: Left and Right (Home, End) move along the row, Up goes
        // back to the search.
        onCardKeydown(e) {
            const cards = this.visibleCards();
            const at = cards.indexOf(e.currentTarget.closest('[data-lm-card]'));
            let next;
            if (e.key === 'ArrowRight') next = at + 1;
            else if (e.key === 'ArrowLeft') next = at - 1;
            else if (e.key === 'Home') next = 0;
            else if (e.key === 'End') next = cards.length - 1;
            else if (e.key === 'ArrowUp') {
                e.preventDefault();
                const search = this.gallery().querySelector('[data-lm-gallery-search]');
                if (search) search.focus();
                return;
            } else {
                return;
            }
            e.preventDefault();
            if (next >= 0 && next < cards.length) this.focusCard(cards[next]);
        },

        focusCard(card) {
            const hit = card && card.querySelector('.lm-card-hit');
            if (!hit) return;
            hit.focus({ preventScroll: true });
            card.scrollIntoView({ block: 'nearest', inline: 'nearest', behavior: motionOff() ? 'auto' : 'smooth' });
        },

        onGalleryKeydown(e) {
            if (e.key !== 'Escape' || e.defaultPrevented || this._drag) return;
            e.preventDefault();
            this.closeGallery();
        },

        // Escape elsewhere on the page puts the gallery away too, unless a
        // field (or anything else) took it.
        onGalleryEscape(e) {
            if (e.key !== 'Escape' || e.defaultPrevented || !this.galleryOpen || this._drag) return;
            if (e.target && e.target.closest && e.target.closest(EDITABLE)) return;
            this.closeGallery();
        },

        onCardClick(e) {
            // The click that ends a drag adds nothing more. A keyboard's click
            // (Enter, Space) has no detail.
            if (e.detail > 0 && performance.now() - this._dragUpAt < 400) return;
            const card = e.currentTarget.closest('[data-lm-card]');
            if (card) this.addCard(card);
        },

        // addCard adds the card's block after the last, and shows it.
        addCard(card) {
            if (!this.editMode) return;
            const key = card.dataset.key;
            const layout = this.currentLayout;
            this.pulse(card, 'is-adding', 360);
            this.enqueue(async () => {
                if (layout !== this.currentLayout || !this.grid()) return;
                const block = await this.insertBlock(key, -1, null);
                if (!block) return;
                const smooth = !motionOff();
                if (this.inView(block)) this.flyIn(card, block);
                block.scrollIntoView({ block: 'nearest', behavior: smooth ? 'smooth' : 'auto' });
            });
        },

        // inView reports whether el shows whole, above the sheet.
        inView(el) {
            const rect = el.getBoundingClientRect();
            const sheet = this.gallery();
            const bottom = sheet && this.galleryOpen ? sheet.getBoundingClientRect().top : window.innerHeight;
            return rect.top >= 0 && rect.bottom <= bottom;
        },

        // flyIn sends a picture of the card to the block it added.
        flyIn(card, block) {
            if (motionOff() || !this.galleryOpen) return;
            const preview = card.querySelector('[data-lm-card-preview]');
            if (!preview) return;
            const from = preview.getBoundingClientRect();
            const to = block.getBoundingClientRect();
            if (!from.width || !to.width) return;
            const ghost = staticClone(preview);
            ghost.classList.add('lm-drag-ghost', 'lm-fly');
            ghost.style.width = from.width + 'px';
            ghost.style.height = from.height + 'px';
            ghost.style.transformOrigin = '0 0';
            document.body.appendChild(ghost);
            const flight = ghost.animate([
                { translate: from.left + 'px ' + from.top + 'px', scale: '1', opacity: 0.95 },
                { translate: to.left + 'px ' + to.top + 'px', scale: String(to.width / from.width), opacity: 0 },
            ], { duration: 460, easing: 'cubic-bezier(.32, .72, 0, 1)', fill: 'forwards' });
            flight.onfinish = () => ghost.remove();
            flight.oncancel = () => ghost.remove();
        },

        // pulse sets className on el for ms: an animation's length.
        pulse(el, className, ms) {
            el.classList.remove(className);
            void el.offsetWidth;
            el.classList.add(className);
            window.setTimeout(() => el.classList.remove(className), ms);
        },

        // --- dragging a card onto the grid ---------------------------------------
        //
        // A card dragged out of the gallery opens a slot in the grid where its
        // block will land, under the pointer, and the blocks around it slide
        // aside to make room. Meanwhile the sheet steps down to a strip:
        // releasing over the strip, or away from the grid, or pressing
        // Escape, puts nothing down. Near the top or bottom of the page the
        // page scrolls. On a touch screen a card lifts when dragged upwards;
        // sideways, the row scrolls.

        onCardPointerDown(e) {
            if (!this.editMode || this._drag) return;
            if (e.pointerType === 'mouse' && e.button !== 0) return;
            const card = e.currentTarget.closest('[data-lm-card]');
            if (!card) return;
            const drag = {
                card,
                key: card.dataset.key,
                label: card.dataset.label || '',
                pointerId: e.pointerId,
                touch: e.pointerType !== 'mouse',
                startX: e.clientX,
                startY: e.clientY,
                x: e.clientX,
                y: e.clientY,
                started: false,
                ghost: null,
                slot: null,
                // The grid's blocks as laid out without the slot (dropBoxes).
                base: null,
                scroller: null,
                // Whether the pointer has been clear of the band above the
                // strip (see autoScrollSpeed).
                armed: false,
                frame: 0,
                dirty: true,
            };
            drag.move = (ev) => this.onDragMove(ev);
            drag.up = (ev) => this.onDragUp(ev);
            drag.cancel = (ev) => this.onDragCancel(ev);
            drag.keydown = (ev) => {
                if (ev.key !== 'Escape') return;
                ev.preventDefault();
                ev.stopPropagation();
                this.cancelDrag();
            };
            this._drag = drag;
            document.addEventListener('pointermove', drag.move, { passive: false });
            document.addEventListener('pointerup', drag.up);
            document.addEventListener('pointercancel', drag.cancel);
            window.addEventListener('keydown', drag.keydown, true);
            window.addEventListener('blur', drag.cancel);
        },

        onDragMove(e) {
            const drag = this._drag;
            if (!drag || e.pointerId !== drag.pointerId) return;
            drag.x = e.clientX;
            drag.y = e.clientY;
            if (!drag.started) {
                const dx = drag.x - drag.startX;
                const dy = drag.y - drag.startY;
                if (drag.touch) {
                    // Sideways the row scrolls; upwards the card lifts.
                    if (Math.abs(dx) > 10 && Math.abs(dx) > Math.abs(dy)) {
                        this.endDrag();
                        return;
                    }
                    if (-dy < 10 || Math.abs(dx) > -dy) return;
                } else if (Math.hypot(dx, dy) < DRAG_THRESHOLD) {
                    return;
                }
                this.beginDrag(drag);
            }
            e.preventDefault();
            drag.dirty = true;
        },

        beginDrag(drag) {
            drag.started = true;
            drag.scroller = scrollParent(this.grid());
            const stage = drag.card.querySelector('[data-lm-card-stage]');
            drag.natural = drag.card.dataset.lmPreview === 'ready' && stage ? stage.offsetHeight : 0;
            drag.previewWidth = this._previews ? this._previews.width : this.previewWidth();
            drag.chrome = this.blockChrome();
            drag.ghost = this.makeGhost(drag);
            drag.card.classList.add('is-lifted');
            document.documentElement.classList.add('lm-dragging-card');
            this.galleryDragging = true;
            const frame = () => {
                if (this._drag !== drag) return;
                this.dragFrame(drag);
                drag.frame = window.requestAnimationFrame(frame);
            };
            drag.frame = window.requestAnimationFrame(frame);
        },

        // dragFrame runs once a frame while a card is dragged: it scrolls the
        // page near its edges and follows the pointer.
        dragFrame(drag) {
            const speed = this.autoScrollSpeed(drag);
            if (speed !== 0) {
                const before = drag.scroller.scrollTop;
                drag.scroller.scrollTop = before + speed;
                if (drag.scroller.scrollTop !== before) drag.dirty = true;
            }
            if (!drag.dirty) return;
            drag.dirty = false;
            this.trackDrag(drag);
        },

        // trackDrag follows the pointer with the ghost, the sheet's strip and
        // the slot.
        trackDrag(drag) {
            const sheet = this.gallery();
            const strip = sheet ? sheet.getBoundingClientRect() : null;
            const overSheet = !!strip && drag.y >= strip.top && drag.x >= strip.left && drag.x <= strip.right;
            this.galleryOverSheet = overSheet;
            const index = overSheet ? -1 : this.dropIndex(drag);
            if (index < 0) this.removeSlot(drag);
            else this.placeSlot(drag, index);
            this.moveGhost(drag);
        },

        // autoScrollSpeed is how far to scroll the page this frame: up near
        // its top, down just above the sheet's strip. Scrolling down waits
        // until the pointer has been above that band once, as a card lifted
        // from low in the sheet starts out in it.
        autoScrollSpeed(drag) {
            const scroller = drag.scroller;
            if (!scroller) return 0;
            const page = scroller === document.scrollingElement || scroller === document.documentElement;
            const rect = page ? { top: 0, bottom: window.innerHeight } : scroller.getBoundingClientRect();
            const top = Math.max(0, rect.top);
            // Where the strip will be, not where the sliding sheet is now.
            const hint = this.gallery() && this.gallery().querySelector('.lm-gallery-drag-hint');
            const bottom = Math.min(window.innerHeight, rect.bottom) - (hint ? hint.offsetHeight : 0);
            const ramp = (depth) => Math.ceil(AUTOSCROLL_MAX_SPEED * Math.min(1, depth / AUTOSCROLL_ZONE));
            if (drag.y < bottom - AUTOSCROLL_ZONE) drag.armed = true;
            if (drag.y < top + AUTOSCROLL_ZONE) return -ramp(top + AUTOSCROLL_ZONE - drag.y);
            if (drag.armed && drag.y > bottom - AUTOSCROLL_ZONE && drag.y < bottom) return ramp(drag.y - (bottom - AUTOSCROLL_ZONE));
            return 0;
        },

        // dropIndex is where a block released at the pointer goes among the
        // blocks of the grid shown: before or after the nearest block, by the
        // side of it the pointer is on; first above the blocks, last below
        // them all. -1 away from the grid. The pointer is matched against the
        // grid as it is without the slot (see dropBoxes): the room the slot
        // makes never moves the slot itself, so it cannot flicker between two
        // places under a still pointer, and where it lands depends only on
        // where the pointer is.
        dropIndex(drag) {
            const grid = this.grid();
            if (!grid) return -1;
            const rect = grid.getBoundingClientRect();
            if (drag.x < rect.left - 48 || drag.x > rect.right + 48) return -1;
            const boxes = this.dropBoxes(drag, grid);
            if (boxes.length === 0) return 0;
            const px = drag.x - rect.left - grid.clientLeft;
            const py = drag.y - rect.top - grid.clientTop;
            if (py < 0) return 0;
            if (boxes.every(b => py > b.b)) return boxes.length;
            let nearest = -1;
            let distance = Infinity;
            boxes.forEach((b, i) => {
                const dx = px < b.l ? b.l - px : px > b.r ? px - b.r : 0;
                const dy = py < b.t ? b.t - py : py > b.b ? py - b.b : 0;
                const d = dx * dx + dy * dy;
                if (d < distance) {
                    distance = d;
                    nearest = i;
                }
            });
            const b = boxes[nearest];
            if (distance === 0) {
                // A block as wide as the grid is split top and bottom.
                if (b.r - b.l > grid.clientWidth * 0.7) return py < (b.t + b.b) / 2 ? nearest : nearest + 1;
                return px < (b.l + b.r) / 2 ? nearest : nearest + 1;
            }
            if (py > b.b) return nearest + 1;
            if (py < b.t) return nearest;
            return px > b.r ? nearest + 1 : nearest;
        },

        // dropBoxes are the blocks' boxes in grid coordinates (offsetLeft and
        // offsetTop: where a block is laid out, not where a sliding one is
        // drawn), as the grid lays them out without the slot. While the slot
        // is out they are read afresh; while it is in, the ones read before it
        // opened are kept, and read again (with the slot taken out for the
        // moment) if the grid changed meanwhile: another page shown, a block
        // added or gone, a new width.
        dropBoxes(drag, grid) {
            const blocks = this.blockElements();
            const slot = drag.slot && drag.slot.parentElement === grid ? drag.slot : null;
            const base = drag.base;
            if (slot && base && base.grid === grid && base.count === blocks.length && base.width === grid.clientWidth) {
                return base.boxes;
            }
            const next = slot ? slot.nextSibling : null;
            if (slot) slot.remove();
            drag.base = {
                grid,
                count: blocks.length,
                width: grid.clientWidth,
                boxes: blocks.map(el => ({
                    l: el.offsetLeft,
                    t: el.offsetTop,
                    r: el.offsetLeft + el.offsetWidth,
                    b: el.offsetTop + el.offsetHeight,
                })),
            };
            if (slot) grid.insertBefore(slot, next);
            return drag.base.boxes;
        },

        // slotIndex is how many blocks come before the slot.
        slotIndex(slot) {
            let n = 0;
            for (let el = slot.previousElementSibling; el; el = el.previousElementSibling) {
                if (el.matches('[data-lm-block]')) n++;
            }
            return n;
        },

        placeSlot(drag, index) {
            const grid = this.grid();
            const slot = drag.slot;
            const current = slot && slot.parentElement === grid ? this.slotIndex(slot) : -1;
            if (index === current) return;
            if (!drag.slot) drag.slot = this.makeSlot(drag);
            this.flip(grid, () => {
                const blocks = this.blockElements();
                if (index < blocks.length) grid.insertBefore(drag.slot, blocks[index]);
                else grid.appendChild(drag.slot);
                this.fitSlot(drag);
            });
        },

        removeSlot(drag) {
            const slot = drag.slot;
            if (!slot || !slot.parentElement) return;
            this.flip(slot.parentElement, () => slot.remove());
        },

        // makeSlot builds the slot: the card's block, as large as it will be
        // placed, faded behind a dashed outline, under its name.
        makeSlot(drag) {
            const slot = document.createElement('div');
            slot.className = 'lm-drop-slot';
            slot.setAttribute('data-lm-drop-slot', '');
            slot.setAttribute('x-ignore', '');
            slot.setAttribute('aria-hidden', 'true');
            slot.style.setProperty('--lm-slot-chrome', drag.chrome + 'px');
            const stage = drag.card.querySelector('[data-lm-card-stage]');
            if (stage && drag.natural > 0) {
                const preview = staticClone(stage);
                preview.className = 'lm-drop-slot-preview';
                preview.style.width = drag.previewWidth + 'px';
                preview.style.transform = '';
                slot.appendChild(preview);
            }
            const label = document.createElement('span');
            label.className = 'lm-drop-slot-label';
            label.textContent = drag.label;
            slot.appendChild(label);
            return slot;
        },

        // fitSlot gives the slot the masonry rows the block will span.
        fitSlot(drag) {
            const slot = drag.slot;
            const grid = slot.parentElement;
            if (!grid) return;
            const preview = slot.querySelector('.lm-drop-slot-preview');
            let height = 112;
            if (preview && drag.natural > 0) {
                const scale = slot.clientWidth > 0 ? slot.clientWidth / drag.previewWidth : 1;
                preview.style.transform = 'scale(' + scale.toFixed(4) + ')';
                height = Math.round(drag.natural * scale) + drag.chrome;
            }
            const styles = window.getComputedStyle(grid);
            const rowHeight = parseFloat(styles.getPropertyValue('--lm-masonry-row')) || 8;
            const rowGap = rowGapOf(styles);
            slot.style.gridRowEnd = 'span ' + Math.max(1, Math.ceil((height + rowGap) / (rowHeight + rowGap)));
        },

        // blockChrome is what a placed block adds to its content's height in
        // edit mode: its controls, the gap below them, padding and borders.
        blockChrome() {
            const block = this.blockElements()[0];
            if (!block) return 34;
            const controls = block.querySelector(':scope > .lm-block-controls');
            const styles = window.getComputedStyle(block);
            return (controls ? controls.offsetHeight : 0)
                + (parseFloat(styles.rowGap) || 0)
                + (parseFloat(styles.paddingTop) || 0) + (parseFloat(styles.paddingBottom) || 0)
                + (parseFloat(styles.borderTopWidth) || 0) + (parseFloat(styles.borderBottomWidth) || 0);
        },

        // flip makes a change to the grid and slides each block from where it
        // was drawn to where the change put it.
        flip(grid, change) {
            if (motionOff()) {
                change();
                return;
            }
            const blocks = Array.from(grid.querySelectorAll(':scope > [data-lm-block]'));
            const before = blocks.map(el => el.getBoundingClientRect());
            change();
            blocks.forEach((el, i) => {
                if (!el.isConnected) return;
                el.getAnimations().forEach(animation => {
                    if (animation.id === 'lm-flip') animation.cancel();
                });
                const after = el.getBoundingClientRect();
                const dx = before[i].left - after.left;
                const dy = before[i].top - after.top;
                if (Math.abs(dx) < 1 && Math.abs(dy) < 1) return;
                const slide = el.animate(
                    [{ transform: 'translate(' + dx + 'px, ' + dy + 'px)' }, { transform: 'none' }],
                    { duration: 220, easing: 'cubic-bezier(.2, .8, .2, 1)' },
                );
                slide.id = 'lm-flip';
            });
        },

        // makeGhost is the picture of the card that follows the pointer.
        makeGhost(drag) {
            const preview = drag.card.querySelector('[data-lm-card-preview]');
            const rect = preview.getBoundingClientRect();
            const ghost = staticClone(preview);
            ghost.classList.add('lm-drag-ghost');
            if (drag.natural > 0) {
                const skeleton = ghost.querySelector('.lm-card-skeleton');
                if (skeleton) skeleton.remove();
            }
            ghost.style.width = rect.width + 'px';
            ghost.style.height = rect.height + 'px';
            drag.offsetX = drag.startX - rect.left;
            drag.offsetY = drag.startY - rect.top;
            ghost.style.transformOrigin = drag.offsetX + 'px ' + drag.offsetY + 'px';
            ghost.style.translate = rect.left + 'px ' + rect.top + 'px';
            document.body.appendChild(ghost);
            return ghost;
        },

        // The ghost keeps the point it was picked up by under the pointer,
        // and shrinks over the grid so the slot shows.
        moveGhost(drag) {
            const ghost = drag.ghost;
            if (!ghost) return;
            ghost.style.translate = (drag.x - drag.offsetX) + 'px ' + (drag.y - drag.offsetY) + 'px';
            ghost.classList.toggle('is-over-grid', !!(drag.slot && drag.slot.parentElement));
        },

        onDragUp(e) {
            const drag = this._drag;
            if (!drag || e.pointerId !== drag.pointerId) return;
            if (!drag.started) {
                // A click: onCardClick adds the block.
                this.endDrag();
                return;
            }
            this._dragUpAt = performance.now();
            drag.x = e.clientX;
            drag.y = e.clientY;
            this.trackDrag(drag);
            const slot = drag.slot && drag.slot.parentElement ? drag.slot : null;
            if (slot) {
                // The slot stays: it becomes the block.
                drag.slot = null;
                this.dropCard(drag, slot);
            } else {
                this.dropGhost(drag.ghost, null);
            }
            drag.ghost = null;
            this.endDrag();
        },

        onDragCancel(e) {
            const drag = this._drag;
            if (!drag || (e && e.type === 'pointercancel' && e.pointerId !== drag.pointerId)) return;
            this.cancelDrag();
        },

        // cancelDrag puts the card back: nothing is added.
        cancelDrag() {
            const drag = this._drag;
            if (!drag) return;
            if (drag.started) {
                // The release still to come is no click.
                document.addEventListener('pointerup', () => { this._dragUpAt = performance.now(); }, { once: true, capture: true });
                this.dropGhost(drag.ghost, null);
                drag.ghost = null;
            }
            this.endDrag();
        },

        endDrag() {
            const drag = this._drag;
            if (!drag) return;
            this._drag = null;
            document.removeEventListener('pointermove', drag.move, { passive: false });
            document.removeEventListener('pointerup', drag.up);
            document.removeEventListener('pointercancel', drag.cancel);
            window.removeEventListener('keydown', drag.keydown, true);
            window.removeEventListener('blur', drag.cancel);
            if (drag.frame) window.cancelAnimationFrame(drag.frame);
            if (drag.slot) this.removeSlot(drag);
            if (drag.ghost) drag.ghost.remove();
            drag.card.classList.remove('is-lifted');
            document.documentElement.classList.remove('lm-dragging-card');
            this.galleryDragging = false;
            this.galleryOverSheet = false;
        },

        // dropCard turns the slot into the card's block: the server places
        // the block at the slot's index, and the block takes the slot's place.
        dropCard(drag, slot) {
            const key = drag.key;
            const layout = this.currentLayout;
            slot.classList.add('is-pending');
            this.dropGhost(drag.ghost, slot);
            this.enqueue(async () => {
                const grid = this.grid();
                if (!grid || slot.parentElement !== grid || layout !== this.currentLayout || !this.editMode) {
                    slot.remove();
                    return;
                }
                await this.insertBlock(key, this.slotIndex(slot), slot);
            });
        },

        // dropGhost lets go of the ghost: into the slot, or away.
        dropGhost(ghost, slot) {
            if (!ghost) return;
            if (motionOff()) {
                ghost.remove();
                return;
            }
            const from = { translate: ghost.style.translate, scale: window.getComputedStyle(ghost).scale, opacity: 1 };
            let to = { scale: '0.85', opacity: 0 };
            if (slot) {
                const target = slot.getBoundingClientRect();
                const scale = target.width / (ghost.offsetWidth || 1);
                const [ox, oy] = ghost.style.transformOrigin.split(' ').map(parseFloat);
                to = {
                    translate: (target.left - ox * (1 - scale)) + 'px ' + (target.top - oy * (1 - scale)) + 'px',
                    scale: String(scale),
                    opacity: 0,
                };
            }
            if (from.scale === 'none') from.scale = '1';
            const fall = ghost.animate([from, to], { duration: slot ? 240 : 180, easing: 'cubic-bezier(.2, .8, .2, 1)', fill: 'forwards' });
            fall.onfinish = () => ghost.remove();
            fall.oncancel = () => ghost.remove();
        },
    });

    const layoutManagerBlock = () => ({
        id: '',
        cols: 1,
        // Distinct names from the page component's: Alpine resolves this.x
        // through the block's scope first, then the page's.
        _el: null,
        _endpoints: null,
        _observer: null,
        _resize: null,

        init() {
            const el = this.$el;
            this._el = el;
            this.id = el.dataset.id || '';
            this.cols = parseInt(el.dataset.cols || '1', 10) || 1;
            this._endpoints = {
                remove: el.dataset.removeUrl || '',
                resize: el.dataset.resizeUrl || '',
                move: el.dataset.moveUrl || '',
            };
            // Masonry: the block spans as many small grid rows as its content
            // and controls need, measured again whenever either changes size.
            this._observer = new ResizeObserver(() => this.fitRows());
            el.querySelectorAll(':scope > .lm-block-content, :scope > .lm-block-controls')
                .forEach(part => this._observer.observe(part));
            this.fitRows();
        },

        destroy() {
            if (this._observer) this._observer.disconnect();
            this._observer = null;
            this.endResize();
        },

        atStart() {
            return this.order.indexOf(this.id) <= 0;
        },

        atEnd() {
            const i = this.order.indexOf(this.id);
            return i < 0 || i === this.order.length - 1;
        },

        // --- edits ------------------------------------------------------------------

        remove() {
            const el = this._el;
            const url = this._endpoints.remove;
            this.enqueue(async () => {
                // A second click on a block already removed sends nothing.
                if (!el.isConnected) return;
                const response = await this.post(url);
                if (!response.ok) return;
                el.remove();
                this.syncOrder();
            });
        },

        shrink() { this.resizeTo('&delta=-1'); },
        grow() { this.resizeTo('&delta=1'); },
        toggleFull() { this.resizeTo('&cols=0'); },

        // resizeTo asks for a new span and shows the one the server stored.
        resizeTo(query, fallback) {
            const el = this._el;
            const url = this._endpoints.resize + query;
            this.enqueue(async () => {
                if (!el.isConnected) return;
                const response = await this.post(url);
                const stored = parseInt(response.headers.get('X-Layout-Manager-Cols') || '', 10);
                if (response.ok && stored > 0) {
                    this.cols = stored;
                } else if (fallback) {
                    this.cols = fallback;
                }
            });
        },

        moveLeft() { this.moveBy(-1); },
        moveRight() { this.moveBy(1); },

        moveBy(delta) {
            const el = this._el;
            const url = this._endpoints.move + '&delta=' + delta;
            this.enqueue(async () => {
                if (!el.isConnected) return;
                const response = await this.post(url);
                if (!response.ok) return;
                // Move the neighbour across the block rather than the block, so
                // a focused control inside it keeps focus.
                const isBlock = node => node && node.matches && node.matches('[data-lm-block]');
                const neighbour = delta < 0 ? el.previousElementSibling : el.nextElementSibling;
                if (isBlock(neighbour)) {
                    if (delta < 0) el.after(neighbour);
                    else el.before(neighbour);
                }
                this.syncOrder();
            });
        },

        // --- drag to resize -----------------------------------------------------------

        startResize(e) {
            if (!this.editMode || (e.button !== undefined && e.button !== 0)) return;
            const el = this._el;
            const grid = el.parentElement;
            if (!grid) return;
            e.preventDefault();
            e.stopPropagation();

            const styles = window.getComputedStyle(grid);
            const columns = Math.max(1, styles.gridTemplateColumns.split(' ').length);
            const gap = parseFloat(styles.columnGap) || 0;
            const unit = (grid.clientWidth + gap) / columns;
            const startColumn = Math.max(0, Math.round((el.getBoundingClientRect().left - grid.getBoundingClientRect().left) / unit));
            this._resize = {
                grid,
                columns,
                unit,
                startColumn,
                startX: e.clientX,
                startCols: this.cols,
                move: (event) => this.previewResize(event),
                end: (event) => this.finishResize(event),
            };
            grid.classList.add('lm-resizing');
            el.classList.add('lm-block-resizing');
            this.showGuide();
            document.addEventListener('pointermove', this._resize.move);
            document.addEventListener('pointerup', this._resize.end);
            document.addEventListener('pointercancel', this._resize.end);
        },

        previewResize(e) {
            const state = this._resize;
            if (!state) return;
            e.preventDefault();
            // Up to the full grid, as the + button allows: a block too wide for
            // its row moves to the next one.
            const delta = Math.round((e.clientX - state.startX) / state.unit);
            this.cols = Math.max(1, Math.min(state.startCols + delta, state.columns));
            this.showGuide();
        },

        // The guide highlights the columns the block will cover.
        showGuide() {
            const state = this._resize;
            if (!state) return;
            const start = state.startColumn + this.cols > state.columns ? 0 : state.startColumn;
            state.grid.querySelectorAll('.lm-grid-guide-cell').forEach((cell, index) => {
                cell.classList.toggle('is-active', index >= start && index < start + this.cols);
            });
        },

        finishResize(e) {
            const state = this._resize;
            if (!state) return;
            const cols = this.cols;
            this.endResize();
            if (e.type === 'pointercancel' || cols === state.startCols) {
                this.cols = state.startCols;
                return;
            }
            this.resizeTo('&cols=' + cols, state.startCols);
        },

        endResize() {
            const state = this._resize;
            if (!state) return;
            document.removeEventListener('pointermove', state.move);
            document.removeEventListener('pointerup', state.end);
            document.removeEventListener('pointercancel', state.end);
            state.grid.classList.remove('lm-resizing');
            this._el.classList.remove('lm-block-resizing');
            state.grid.querySelectorAll('.lm-grid-guide-cell').forEach(cell => cell.classList.remove('is-active'));
            this._resize = null;
        },

        // --- masonry -------------------------------------------------------------------

        fitRows() {
            const el = this._el;
            const grid = el.parentElement;
            if (!grid) return;
            const gridStyles = window.getComputedStyle(grid);
            const rowHeight = parseFloat(gridStyles.getPropertyValue('--lm-masonry-row')) || 8;
            const rowGap = rowGapOf(gridStyles);
            const styles = window.getComputedStyle(el);
            const content = el.querySelector(':scope > .lm-block-content');
            const controls = el.querySelector(':scope > .lm-block-controls');
            // Laid-out heights, not scrollHeight: an open popover or menu
            // positioned over the page (a table's column list) would count in
            // scrollHeight, push the blocks below down and leave the gap once
            // it closes, as nothing resizes to measure again.
            const controlsHeight = controls && controls.offsetParent !== null ? controls.offsetHeight : 0;
            const contentHeight = content ? content.offsetHeight : 0;
            const innerGap = controlsHeight > 0 && contentHeight > 0 ? parseFloat(styles.rowGap) || 0 : 0;
            const chrome = (parseFloat(styles.paddingTop) || 0) + (parseFloat(styles.paddingBottom) || 0)
                + (parseFloat(styles.borderTopWidth) || 0) + (parseFloat(styles.borderBottomWidth) || 0);
            const height = Math.max(parseFloat(styles.minHeight) || 0, controlsHeight + contentHeight + innerGap + chrome);
            el.style.gridRowEnd = 'span ' + Math.max(1, Math.ceil((height + rowGap) / (rowHeight + rowGap)));
        },
    });

    const register = () => {
        if (!window.Alpine) return false;
        window.Alpine.data('layout_manager', layoutManager);
        window.Alpine.data('layout_manager_block', layoutManagerBlock);
        return true;
    };

    if (!register()) {
        document.addEventListener('alpine:init', register, { once: true });
    }
})();
