import { formatMessage, bodyNotice, buildCurl, formatRoute } from './inspector.mjs';
import { attachSplitter } from './layout.mjs';
import { buildHAR } from './har.mjs';
import { PendingTraffic, streamLabel } from './traffic.mjs';

const MAX_EVENTS = 200;
const MOCK_CONFIG = JSON.parse(document.getElementById("mock-config").textContent);
const themeToggle = document.getElementById('themeToggle');
const themeToggleLabel = document.getElementById('themeToggleLabel');
const prefersDarkScheme = window.matchMedia('(prefers-color-scheme: dark)');
const requestTableBody = document.getElementById('requestTableBody');
const requestTable = requestTableBody.closest('table');
const requestDetails = document.getElementById('requestDetails');
const responseDetails = document.getElementById('responseDetails');
const clearRequestsButton = document.getElementById('clearRequestsButton');
const clearError = document.getElementById('clearError');
const pauseButton = document.getElementById('pauseButton');
const pauseButtonLabel = pauseButton.querySelector('.button-label');
const exportButton = document.getElementById('exportButton');
const helpButton = document.getElementById('helpButton');
const helpDialog = document.getElementById('helpDialog');
const helpCloseButton = document.getElementById('helpCloseButton');
const filterInput = document.getElementById('filterInput');
const routesList = document.getElementById('routesList');
const refreshRoutesButton = document.getElementById('refreshRoutesButton');
const routesToggleButton = document.getElementById('routesToggleButton');
const routesToggleLabel = document.getElementById('routesToggleLabel');
const requestPanel = document.querySelector('.request-panel');
const requestCount = document.getElementById('requestCount');
const routeCount = document.getElementById('routeCount');
const streamStatus = document.getElementById('streamStatus');
const workspace = document.getElementById('workspace');
const inspectorTitle = document.getElementById('inspectorTitle');
const matchSummary = document.getElementById('matchSummary');
const exchangeView = document.getElementById('exchangeView');
const routeView = document.getElementById('routeView');
const routeDetails = document.getElementById('routeDetails');
const copyNotice = document.getElementById('copyNotice');
const copyCurlButton = document.getElementById('copyCurlButton');
const mismatchPanel = document.getElementById('mismatchPanel');
const mismatchDetails = document.getElementById('mismatchDetails');
const backToList = document.getElementById('backToList');
const resetResponsesButton = document.getElementById('resetResponsesButton');
const configBanner = document.getElementById('configBanner');
const reloadErrorPanel = document.getElementById('reloadErrorPanel');
const reloadError = document.getElementById('reloadError');
const pretty = { request: true, response: true };
let selectedRoute = null;
let returnFocus = null;
let copyGeneration = 0;

/** @type {Map<number, any>} */
const eventsById = new Map();
/** @type {number[]} newest-first ids */
let eventOrder = [];
let paused = false;
let selectedId = null;
let filterText = '';
let streamSession = null;
let clearedThrough = 0;
const pending = new PendingTraffic(MAX_EVENTS);
let connected = false;
let renderPending = false;
let streamNotice = '';
const preferences = {
    get(key) { try { return localStorage.getItem(key); } catch { return null; } },
    set(key, value) { try { localStorage.setItem(key, value); } catch { /* Storage is optional. */ } },
};
attachSplitter(document.getElementById('workspaceSplitter'), workspace,
    { axis: 'x', property: '--left-width', key: 'leftWidth', initial: 45, min: 25, max: 75 }, preferences);
attachSplitter(document.getElementById('routesSplitter'), requestPanel,
    { axis: 'y', property: '--traffic-height', key: 'trafficHeight', initial: 70, min: 30, max: 85 }, preferences);
attachSplitter(document.getElementById('exchangeSplitter'), exchangeView,
    { axis: 'y', property: '--request-height', key: 'requestHeight', initial: 50, min: 20, max: 80 }, preferences);

function updateStreamStatus() {
    streamStatus.textContent = streamLabel(connected, paused, pending.events.size, pending.dropped) + streamNotice;
}
function scheduleRender() {
    if (renderPending) return;
    renderPending = true;
    requestAnimationFrame(() => { renderPending = false; updateRequestCount(); renderTable(); });
}

function setTheme(theme) {
    if (theme === 'dark') {
        document.documentElement.setAttribute('data-theme', 'dark');
        themeToggleLabel.textContent = 'Light';
        preferences.set('theme', 'dark');
    } else {
        document.documentElement.removeAttribute('data-theme');
        themeToggleLabel.textContent = 'Dark';
        preferences.set('theme', 'light');
    }
}

function setRoutesCollapsed(collapsed) {
    requestPanel.classList.toggle('routes-collapsed', collapsed);
    routesToggleButton.setAttribute('aria-expanded', String(!collapsed));
    routesToggleButton.setAttribute('aria-label', collapsed ? 'Show configured routes' : 'Hide configured routes');
    routesToggleLabel.textContent = collapsed ? 'Show' : 'Hide';
    preferences.set('routesCollapsed', collapsed ? 'true' : 'false');
}

const savedTheme = preferences.get('theme');
if (savedTheme === 'dark') {
    setTheme('dark');
} else if (savedTheme === 'light') {
    setTheme('light');
} else if (prefersDarkScheme.matches) {
    setTheme('dark');
}
setRoutesCollapsed(preferences.get('routesCollapsed') === 'true');

themeToggle.addEventListener('click', () => {
    if (document.documentElement.getAttribute('data-theme') === 'dark') {
        setTheme('light');
    } else {
        setTheme('dark');
    }
});

const source = new EventSource('events');
function clearThrough(cursor = Infinity) {
    pending.clearThrough(cursor);
    for (const id of eventOrder) { if (id <= cursor) eventsById.delete(id); }
    eventOrder = eventOrder.filter(id => eventsById.has(id));
    if (!eventsById.has(selectedId)) {
        selectedId = null;
        updateDetail(requestDetails, '');
        updateDetail(responseDetails, '');
    }
    updateRequestCount();
    renderTable();
    updateStreamStatus();
}
source.addEventListener('reset', event => {
    const data = JSON.parse(event.data);
    streamNotice = data.reason === 'history-gap' ? ' · History gap; showing retained traffic' : '';
    streamSession = data.session;
    clearedThrough = 0;
    clearThrough();
});
source.addEventListener('clear', event => {
    const data = JSON.parse(event.data);
    streamNotice = '';
    streamSession = data.session;
    clearedThrough = Math.max(clearedThrough, data.id);
    clearThrough(clearedThrough);
});
source.onopen = function () {
    connected = true;
    document.body.classList.remove('stream-offline');
    updateStreamStatus();
};
source.onerror = function () {
    connected = false;
    document.body.classList.add('stream-offline');
    updateStreamStatus();
};
source.onmessage = function (event) {
    try {
        const data = JSON.parse(event.data);
        if (streamSession !== data.session) {
            clearThrough();
            clearedThrough = 0;
            streamSession = data.session;
        }
        if (data.id > clearedThrough) {
            if (paused) pending.add(data);
            else upsertEvent(data);
            updateStreamStatus();
        }
    } catch (e) {
        console.error('Error parsing json', e);
    }
};

function getDataRows() {
    return [...requestTableBody.querySelectorAll('tr[data-id]')];
}

function syncRowTabindex() {
    const rows = getDataRows();
    let focusTarget = null;
    for (const row of rows) {
        const isSelected = selectedId != null && row.dataset.id === String(selectedId);
        row.tabIndex = -1;
        row.setAttribute('aria-selected', isSelected ? 'true' : 'false');
        if (isSelected) {
            focusTarget = row;
        }
    }
    if (!focusTarget && rows[0]) {
        focusTarget = rows[0];
    }
    if (focusTarget) {
        focusTarget.tabIndex = 0;
    }
    return focusTarget;
}

function selectRow(row, http) {
    selectedId = http.id;
    for (const r of requestTableBody.querySelectorAll('tr[data-id]')) {
        const isSelected = r === row;
        r.classList.toggle('selected', isSelected);
        r.setAttribute('aria-selected', isSelected ? 'true' : 'false');
        r.tabIndex = isSelected ? 0 : -1;
    }
    selectedRoute = null;
    for (const button of routesList.querySelectorAll('button')) button.setAttribute('aria-pressed', 'false');
    returnFocus = row;
    showInspector();
    renderInspector();
}

function selectRowByOffset(currentRow, index) {
    const rows = getDataRows();
    if (!rows.length) {
        return;
    }
    const next = rows[Math.max(0, Math.min(index, rows.length - 1))];
    if (!next) {
        return;
    }
    const http = eventsById.get(Number(next.dataset.id));
    if (!http) {
        return;
    }
    selectRow(next, http);
    if (!window.matchMedia('(max-width: 900px)').matches) next.focus();
    next.scrollIntoView({ block: 'nearest' });
}

requestTableBody.addEventListener('click', (e) => {
    const row = e.target.closest('tr');
    if (!row || !row.dataset.id) {
        return;
    }
    const id = Number(row.dataset.id);
    const http = eventsById.get(id);
    if (http) {
        selectRow(row, http);
        if (!window.matchMedia('(max-width: 900px)').matches) row.focus();
    }
});

requestTableBody.addEventListener('keydown', (event) => {
    if (event.target.matches('input, textarea, [contenteditable="true"]')) {
        return;
    }
    const row = event.target.closest('tr[data-id]');
    if (!row || !requestTableBody.contains(row)) {
        return;
    }
    const rows = getDataRows();
    const index = rows.indexOf(row);
    if (index === -1) {
        return;
    }
    if (event.key === 'ArrowDown') {
        event.preventDefault();
        selectRowByOffset(row, index + 1);
    } else if (event.key === 'ArrowUp') {
        event.preventDefault();
        selectRowByOffset(row, index - 1);
    } else if (event.key === 'Home') {
        event.preventDefault();
        selectRowByOffset(row, 0);
    } else if (event.key === 'End') {
        event.preventDefault();
        selectRowByOffset(row, rows.length - 1);
    } else if (event.key === 'Enter' || event.key === ' ') {
        event.preventDefault();
        const http = eventsById.get(Number(row.dataset.id));
        if (http) {
            selectRow(row, http);
        }
    }
});

function hideClearError() {
    clearError.hidden = true;
    clearError.textContent = '';
}

function showClearError() {
    clearError.hidden = false;
    clearError.textContent = "Couldn’t clear the log — the server didn’t confirm. Try again.";
}

clearRequestsButton.addEventListener('click', async () => {
    let cursor = 0;
    try {
        const res = await fetch('clear', { method: 'POST', headers: { 'X-Requested-With': 'XMLHttpRequest' }, signal: AbortSignal.timeout(5000) });
        if (!res.ok) {
            console.error('failed to clear server events', res.status);
            showClearError();
            return;
        }
        const responseSession = res.headers.get('X-Mock-Session');
        if (streamSession && responseSession !== streamSession) return;
        cursor = Number(res.headers.get('X-Mock-Cursor'));
    } catch (e) {
        console.error('failed to clear server events', e);
        showClearError();
        return;
    }
    clearedThrough = Math.max(clearedThrough, cursor);
    clearThrough(clearedThrough);
    hideClearError();
});

pauseButton.addEventListener('click', () => {
    paused = !paused;
    if (!paused) { for (const event of pending.drain()) upsertEvent(event); }
    pauseButtonLabel.textContent = paused ? 'Resume' : 'Pause';
    pauseButton.classList.toggle('active', paused);
    pauseButton.setAttribute('aria-pressed', String(paused));
    pauseButton.setAttribute('aria-label', paused ? 'Resume request log' : 'Pause request log');
    updateStreamStatus();
    document.body.classList.toggle('stream-paused', paused);
});

exportButton.addEventListener('click', () => {
    const har = buildHAR([...eventOrder].reverse().map((id) => eventsById.get(id)).filter(Boolean), MOCK_CONFIG.version);
    const blob = new Blob([JSON.stringify(har, null, 2)], { type: 'application/json' });
    const url = URL.createObjectURL(blob);
    const a = document.createElement('a');
    a.href = url;
    a.download = `mock-${new Date().toISOString().replace(/[:.]/g, '-')}.har`;
    a.click();
    setTimeout(() => URL.revokeObjectURL(url), 1000);
});

function openHelp() {
    if (typeof helpDialog.showModal !== 'function' || helpDialog.open) {
        return;
    }
    const body = helpDialog.querySelector('.help-dialog-body');
    if (body) {
        body.scrollTop = 0;
    }
    helpDialog.showModal();
}

function closeHelp() {
    if (helpDialog.open) {
        helpDialog.close();
    }
}

helpButton.addEventListener('click', openHelp);
helpCloseButton.addEventListener('click', closeHelp);
helpDialog.addEventListener('click', (event) => {
    const rect = helpDialog.getBoundingClientRect();
    const inside = event.clientX >= rect.left && event.clientX <= rect.right
        && event.clientY >= rect.top && event.clientY <= rect.bottom;
    if (!inside) {
        closeHelp();
    }
});

filterInput.addEventListener('input', () => {
    filterText = filterInput.value.trim().toLowerCase();
    renderTable();
});

document.addEventListener('keydown', (event) => {
    const typing = event.target.matches('input, textarea, [contenteditable="true"]');
    if (event.key === '/' && !typing && !helpDialog.open) {
        event.preventDefault();
        filterInput.focus();
    }
});

let loadingState = false;
let loadingRoutes = false;
let lastRoutesJSON = null;

refreshRoutesButton.addEventListener('click', loadRoutes);
routesToggleButton.addEventListener('click', () => {
    setRoutesCollapsed(!requestPanel.classList.contains('routes-collapsed'));
});
loadRoutes();
// Refresh routes periodically so hot-reload is visible without a full page refresh.
setInterval(loadRoutes, 3000);
loadState();
setInterval(loadState, 2000);

function upsertEvent(http) {
    if (http.id == null) {
        return;
    }
    const isNew = !eventsById.has(http.id);
    eventsById.set(http.id, http);
    if (isNew) {
        eventOrder.push(http.id);
        eventOrder.sort((a,b) => b-a);
        while (eventOrder.length > MAX_EVENTS) {
            const dropped = eventOrder.pop();
            eventsById.delete(dropped);
        }
    }
    if (selectedId != null && !eventsById.has(selectedId)) {
        selectedId = null;
        updateDetail(requestDetails, '');
        updateDetail(responseDetails, '');
    }
    scheduleRender();
}

function updateRequestCount() {
    const value = String(eventOrder.length).padStart(3, '0');
    requestCount.textContent = `${value}/${MAX_EVENTS}`;
    requestCount.setAttribute('aria-label', `${eventOrder.length} requests`);
}

function eventMatchesFilter(http) {
    if (!filterText) {
        return true;
    }
    const hay = [
        http.request.method,
        http.request.url,
        String(http.response.status),
        http.response.statusText || '',
    ].join(' ').toLowerCase();
    return hay.includes(filterText);
}

function renderTable() {
    const focusWasInTable = requestTable.contains(document.activeElement);
    const visible = eventOrder
        .map((id) => eventsById.get(id))
        .filter(Boolean)
        .filter(eventMatchesFilter);

    if (visible.length === 0) {
        selectedId = null;
        updateDetail(requestDetails, '');
        updateDetail(responseDetails, '');
        renderEmptyState(filterText ? 'No matching requests' : 'No requests yet');
        return;
    }

    const existing = new Map(getDataRows().map(row => [Number(row.dataset.id), row]));
    const visibleIDs = new Set(visible.map(event => event.id));
    for (const row of requestTableBody.querySelectorAll('tr')) {
        if (!row.dataset.id || !visibleIDs.has(Number(row.dataset.id))) row.remove();
    }
    if (selectedId != null && !visibleIDs.has(selectedId)) {
        selectedId = null;
        updateDetail(requestDetails, '');
        updateDetail(responseDetails, '');
    }
    let previous = null;
    for (const http of visible) {
        let row = existing.get(http.id);
        if (row) { previous = row; continue; }
        row = document.createElement('tr');
        requestTableBody.insertBefore(row, previous ? previous.nextSibling : requestTableBody.firstChild);
        previous = row;
        row.dataset.id = String(http.id);
        row.tabIndex = -1;
        row.setAttribute('aria-selected', 'false');
        if (selectedId === http.id) {
            row.classList.add('selected');
        }

        const c0 = row.insertCell(0);
        c0.className = 'time-cell';
        c0.textContent = http.request.time || '';

        const c1 = row.insertCell(1);
        const statusSpan = document.createElement('span');
        const statusText = http.response.statusText || '';
        statusSpan.textContent = `${http.response.status}`;
        statusSpan.className = 'status-code';
        if (statusText) {
            statusSpan.title = statusText;
            statusSpan.setAttribute('aria-label', `${http.response.status} ${statusText}`);
        }
        const status = parseInt(http.response.status, 10);
        if (status >= 200 && status < 300) statusSpan.classList.add('status-2xx');
        else if (status >= 300 && status < 400) statusSpan.classList.add('status-3xx');
        else if (status >= 400 && status < 500) statusSpan.classList.add('status-4xx');
        else if (status >= 500) statusSpan.classList.add('status-5xx');
        c1.appendChild(statusSpan);

        const c2 = row.insertCell(2);
        c2.className = 'request-cell';
        const methodSpan = document.createElement('span');
        methodSpan.textContent = http.request.method;
        methodSpan.className = `method-${(http.request.method || '').toLowerCase()}`;
        c2.appendChild(methodSpan);
        const urlSpan = document.createElement('span');
        urlSpan.textContent = ` ${http.request.url}`;
        urlSpan.title = http.request.url;
        row.title = `${http.request.method} ${http.request.url} · ${http.response.time}`;
        c2.appendChild(urlSpan);
    }

    const focusTarget = syncRowTabindex();
    if (focusWasInTable && focusTarget) {
        focusTarget.focus({ preventScroll: true });
    }
}

function updateDetail(element, newContent) {
    element.textContent = newContent;
    if (!newContent && selectedId == null && !selectedRoute) resetInspector();
}

function showInspector() {
    workspace.classList.add('show-inspector');
    copyNotice.textContent = '';
    copyGeneration++;
    if (window.matchMedia('(max-width: 900px)').matches) inspectorTitle.focus();
}

backToList.addEventListener('click', () => {
    workspace.classList.remove('show-inspector');
    if (returnFocus?.isConnected) returnFocus.focus();
    else filterInput.focus();
});

function resetInspector() {
    copyGeneration++;
    inspectorTitle.textContent = 'Exchange inspector';
    matchSummary.textContent = 'Select a request or a configured route.';
    copyCurlButton.disabled = true;
    mismatchPanel.hidden = true;
    copyNotice.textContent = '';
    for (const kind of ['request', 'response']) {
        document.getElementById(`${kind}BodyNotice`).textContent = '';
        document.getElementById(`copy${kind === 'request' ? 'Request' : 'Response'}Body`).disabled = true;
    }
}

function renderInspector() {
    const event = eventsById.get(selectedId);
    exchangeView.hidden = !!selectedRoute;
    routeView.hidden = !selectedRoute;
    if (selectedRoute) {
        inspectorTitle.textContent = selectedRoute.name || 'Route configuration';
        matchSummary.textContent = `Configured route · revision ${selectedRoute.revision} · ${selectedRoute.source}:${selectedRoute.line}`;
        routeDetails.textContent = formatRoute(selectedRoute);
        mismatchPanel.hidden = true;
        copyCurlButton.disabled = true;
        return;
    }
    if (!event) { resetInspector(); return; }
    inspectorTitle.textContent = `${event.request.method} ${event.request.url}`;
    const match = event.match;
    const route = match?.route;
    matchSummary.textContent = route
        ? `${route.name} · ${route.source}:${route.line} · response ${match.position} of ${match.total} · revision ${match.revision} · ${event.response.time}`
        : `No matching route · revision ${match?.revision ?? '?'} · ${event.response.time}`;
    mismatchPanel.hidden = !!route;
    mismatchDetails.replaceChildren();
    if (!route) {
        const candidates = match?.candidates || [];
        if (!candidates.length) mismatchDetails.textContent = 'No routes are configured for this request.';
        for (const candidate of candidates) {
            const heading = document.createElement('p');
            heading.textContent = `${candidate.method} ${candidate.path} — ${candidate.name} (${candidate.source}:${candidate.line})`;
            const reasons = document.createElement('ul');
            for (const reason of candidate.reasons) {
                const item = document.createElement('li'); item.textContent = reason; reasons.append(item);
            }
            mismatchDetails.append(heading, reasons);
        }
    }
    copyCurlButton.disabled = false;
    for (const kind of ['request', 'response']) {
        const message = event[kind];
        document.getElementById(`${kind}Details`).textContent = formatMessage(message, pretty[kind], kind === 'request');
        document.getElementById(`${kind}BodyNotice`).textContent = bodyNotice(message.body);
        document.getElementById(`${kind}Pretty`).setAttribute('aria-pressed', String(pretty[kind]));
        document.getElementById(`${kind}Raw`).setAttribute('aria-pressed', String(!pretty[kind]));
        const copyButton = document.getElementById(`copy${kind === 'request' ? 'Request' : 'Response'}Body`);
        copyButton.disabled = !message.body?.capturedSize;
        copyButton.textContent = message.body?.encoding === 'base64' ? 'Copy base64' : 'Copy body';
    }
}

async function copyText(text) {
    const generation = copyGeneration;
    try {
        if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable; select the content and copy it manually.');
        await navigator.clipboard.writeText(text);
        if (generation === copyGeneration) copyNotice.textContent = '';
    } catch (error) {
        if (generation === copyGeneration) copyNotice.textContent = error.message || 'Copy failed. Select and copy the content manually.';
    }
}

for (const kind of ['request', 'response']) {
    for (const mode of ['Pretty', 'Raw']) {
        document.getElementById(`${kind}${mode}`).addEventListener('click', () => {
            pretty[kind] = mode === 'Pretty'; renderInspector();
        });
    }
    document.getElementById(`copy${kind === 'request' ? 'Request' : 'Response'}Body`).addEventListener('click', () => {
        const body = eventsById.get(selectedId)?.[kind]?.body;
        if (body) copyText(body.text);
    });
}
copyCurlButton.addEventListener('click', () => {
    try { copyText(buildCurl(eventsById.get(selectedId)?.request)); }
    catch (error) { copyNotice.textContent = error.message; }
});

resetResponsesButton.addEventListener('click', async () => {
    resetResponsesButton.disabled = true;
    hideClearError();
    try {
        const response = await fetch('reset', { method: 'POST', headers: { 'X-Requested-With': 'XMLHttpRequest' }, signal: AbortSignal.timeout(5000) });
        if (!response.ok) throw new Error(`Server returned ${response.status}`);
        clearError.hidden = false;
        clearError.textContent = 'Response sequences reset. Traffic was kept.';
    } catch (error) {
        clearError.hidden = false;
        clearError.textContent = `Could not reset responses: ${error.message}`;
    } finally { resetResponsesButton.disabled = false; }
});

function renderEmptyState(message) {
    requestTableBody.replaceChildren();
    const row = requestTableBody.insertRow();
    const cell = row.insertCell();
    cell.colSpan = 3;
    cell.className = 'empty-state';
    cell.textContent = message || 'No requests yet';
}

async function loadRoutes() {
    if (loadingRoutes) return;
    loadingRoutes = true;
    try {
        const res = await fetch('routes', { signal: AbortSignal.timeout(5000) });
        if (!res.ok) {
            throw new Error(`status ${res.status}`);
        }
        const routes = await res.json();
        const routesJSON = JSON.stringify(routes);
        if (routesJSON === lastRoutesJSON) return;
        lastRoutesJSON = routesJSON;
        if (selectedRoute) {
            const current = routes.find(route => route.id === selectedRoute.id);
            if (current) { selectedRoute = current; renderInspector(); }
            else matchSummary.textContent = 'This route belongs to a previous revision. Select a current route to inspect it.';
        }
        routeCount.textContent = String(routes.length).padStart(2, '0');
        routeCount.setAttribute('aria-label', `${routes.length} configured routes`);
        routesList.replaceChildren();
        if (!routes.length) {
            const li = document.createElement('li');
            li.className = 'empty-state';
            li.textContent = 'No routes configured';
            routesList.appendChild(li);
            return;
        }
        for (const route of routes) {
            const li = document.createElement('li');
            const method = document.createElement('span');
            method.className = `method-${(route.method || '').toLowerCase()}`;
            method.textContent = route.method;
            const path = document.createElement('span');
            path.className = 'route-path';
            const routePath = `${route.path}${route.query ? '?' + route.query : ''}`;
            path.textContent = routePath;
            path.title = routePath;
            const name = document.createElement('span');
            name.className = 'route-name';
            name.textContent = route.name || '';
            if (route.name) {
                name.title = route.name;
            }
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'route-button';
            button.setAttribute('aria-label', `${route.method} ${routePath}: ${route.name}`);
            button.setAttribute('aria-pressed', String(selectedRoute?.id === route.id));
            button.append(method, path, name);
            button.addEventListener('click', () => {
                selectedId = null;
                selectedRoute = route;
                returnFocus = button;
                for (const item of routesList.querySelectorAll('button')) item.setAttribute('aria-pressed', String(item === button));
                for (const row of getDataRows()) row.classList.remove('selected');
                syncRowTabindex();
                showInspector();
                renderInspector();
            });
            li.append(button);
            routesList.appendChild(li);
        }
    } catch (e) {
        lastRoutesJSON = null;
        routeCount.textContent = '—';
        routeCount.setAttribute('aria-label', 'Routes unavailable');
        routesList.replaceChildren();
        const li = document.createElement('li');
        li.className = 'empty-state';
        li.textContent = 'Failed to load routes';
        routesList.appendChild(li);
    } finally { loadingRoutes = false; }
}

async function loadState() {
    if (loadingState) return;
    loadingState = true;
    try {
        const response = await fetch('state', { signal: AbortSignal.timeout(5000) });
        if (!response.ok) throw new Error(`status ${response.status}`);
        const state = await response.json();
        configBanner.hidden = !state.error;
        reloadErrorPanel.hidden = !state.error;
        reloadError.textContent = state.error || '';
        if (state.error) reloadErrorPanel.open = true;
    } catch (error) {
        console.error('Failed to load configuration state:', error);
    } finally { loadingState = false; }
}
