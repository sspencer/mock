import { formatMessage, bodyNotice, buildCurl, buildRouteCurl, formatRoute, hitsLabel } from './inspector.mjs';
import { buildRouteDraft, buildRouteDrafts } from './draft.mjs';
import { attachSplitter, alignedTrafficHeight } from './layout.mjs';
import { buildHAR } from './har.mjs';
import { filterTerms, matchesFilter, routeFilter } from './filter.mjs';
import { TrafficStream, requestHost, eventBadge } from './traffic.mjs';
import { noSelection, selectRequest, selectRoute, selectedRequestId, selectedRoute, afterEventsChanged, afterRoutesLoaded } from './selection.mjs';

const MAX_EVENTS = 1000;
const MOCK_CONFIG = JSON.parse(document.getElementById("mock-config").textContent);
// The UI is served on its own port; sample requests target the route's mock port (or the default
// mock port) on the host the UI was reached by.
const mockOrigin = (port) => `http://${location.hostname}:${port}`;
const routePort = (route) => route?.port || MOCK_CONFIG.mockPort;
// Hosts and ports only distinguish traffic when mock listens on more than one port.
const multiPort = Boolean(MOCK_CONFIG.multiPort);

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
const requestTitle = document.getElementById('requestTitle');
const routeTitle = document.getElementById('routeTitle');
const routeSummary = document.getElementById('routeSummary');
const copyRouteCurlButton = document.getElementById('copyRouteCurlButton');
const matchSummary = document.getElementById('matchSummary');
const exchangeView = document.getElementById('exchangeView');
const routeView = document.getElementById('routeView');
const routeDetails = document.getElementById('routeDetails');
const copyNotice = document.getElementById('copyNotice');
const copyCurlButton = document.getElementById('copyCurlButton');
if (MOCK_CONFIG.redact) {
    // -R masked credentials before they reached the log, so copies and exports carry no secrets.
    for (const button of [exportButton, copyCurlButton]) button.title = 'Credentials are redacted (-R)';
}
const copyRouteDraftButton = document.getElementById('copyRouteDraftButton');
const copyUnmatchedButton = document.getElementById('copyUnmatchedButton');
const mismatchPanel = document.getElementById('mismatchPanel');
const mismatchDetails = document.getElementById('mismatchDetails');
const backToList = document.getElementById('backToList');
const configBanner = document.getElementById('configBanner');
const reloadErrorPanel = document.getElementById('reloadErrorPanel');
const reloadError = document.getElementById('reloadError');
const pretty = { request: true, response: true };
let selection = noSelection;
let returnFocus = null;
let copyGeneration = 0;

/** @type {Map<number, any>} */
const eventsById = new Map();
/** @type {number[]} newest-first ids */
let eventOrder = [];
let filterText = '';
let filterParsed = [];
const stream = new TrafficStream(MAX_EVENTS);
let renderPending = false;
const preferences = {
    get(key) { try { return localStorage.getItem(key); } catch { return null; } },
    set(key, value) { try { localStorage.setItem(key, value); } catch { /* Storage is optional. */ } },
};
attachSplitter(document.getElementById('workspaceSplitter'), workspace,
    { axis: 'x', property: '--left-width', key: 'leftWidth', initial: 45, min: 25, max: 75 }, preferences);
attachSplitter(document.getElementById('exchangeSplitter'), exchangeView,
    { axis: 'y', property: '--request-height', key: 'requestHeight', initial: 50, min: 20, max: 80 }, preferences);
// By default the Routes panel is as tall as the Response pane, so the two
// dividers line up. The Request/Response split must be applied first.
const responseSection = responseDetails.closest('.detail-section');
attachSplitter(document.getElementById('routesSplitter'), requestPanel,
    { axis: 'y', property: '--traffic-height', key: 'trafficHeight', min: 30, max: 85,
      initial: () => alignedTrafficHeight(requestPanel.getBoundingClientRect().height, responseSection?.getBoundingClientRect().height, 70) }, preferences);

function updateStreamStatus() {
    streamStatus.textContent = stream.label();
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
// clearDisplayed removes displayed events with id <= cursor (Infinity = all).
function clearDisplayed(cursor) {
    for (const id of eventOrder) { if (id <= cursor) eventsById.delete(id); }
    eventOrder = eventOrder.filter(id => eventsById.has(id));
    dropMissingSelection();
    updateRequestCount();
    renderTable();
}
// applyStream applies the effect of a TrafficStream transition to the page.
function applyStream(effect) {
    if (!effect) return;
    if (effect.clearThrough != null) clearDisplayed(effect.clearThrough);
    for (const event of effect.show) upsertEvent(event);
    updateStreamStatus();
}
source.addEventListener('reset', event => applyStream(stream.reset(JSON.parse(event.data))));
source.addEventListener('clear', event => applyStream(stream.cleared(JSON.parse(event.data))));
source.onopen = function () {
    stream.opened();
    document.body.classList.remove('stream-offline');
    updateStreamStatus();
};
source.onerror = function () {
    stream.failed();
    document.body.classList.add('stream-offline');
    updateStreamStatus();
};
source.onmessage = function (event) {
    let data;
    try {
        data = JSON.parse(event.data);
    } catch (e) {
        console.error('Error parsing json', e);
        return;
    }
    applyStream(stream.message(data));
};

// dropMissingSelection deselects a request that is no longer displayed.
function dropMissingSelection(isDisplayed = (id) => eventsById.has(id)) {
    const next = afterEventsChanged(selection, isDisplayed);
    if (next === selection) return;
    selection = next;
    updateDetail(requestDetails, '');
    updateDetail(responseDetails, '');
}

function getDataRows() {
    return [...requestTableBody.querySelectorAll('tr[data-id]')];
}

function syncRowTabindex() {
    const rows = getDataRows();
    let focusTarget = null;
    for (const row of rows) {
        const isSelected = row.dataset.id === String(selectedRequestId(selection));
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
    selection = selectRequest(http.id);
    for (const r of requestTableBody.querySelectorAll('tr[data-id]')) {
        const isSelected = r === row;
        r.classList.toggle('selected', isSelected);
        r.setAttribute('aria-selected', isSelected ? 'true' : 'false');
        r.tabIndex = isSelected ? 0 : -1;
    }
    for (const button of routesList.querySelectorAll('.route-button')) button.setAttribute('aria-pressed', 'false');
    returnFocus = row;
    showInspector();
    renderInspector();
    focusInspectorOnMobile();
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
    let effect;
    try {
        const res = await fetch('clear', { method: 'POST', headers: { 'X-Requested-With': 'XMLHttpRequest' }, signal: AbortSignal.timeout(5000) });
        if (!res.ok) {
            console.error('failed to clear server events', res.status);
            showClearError();
            return;
        }
        effect = stream.clearConfirmed(res.headers.get('X-Mock-Session'), Number(res.headers.get('X-Mock-Cursor')));
    } catch (e) {
        console.error('failed to clear server events', e);
        showClearError();
        return;
    }
    if (!effect) return;
    applyStream(effect);
    hideClearError();
});

pauseButton.addEventListener('click', () => {
    applyStream(stream.paused ? stream.resume() : stream.pause());
    const paused = stream.paused;
    pauseButtonLabel.textContent = paused ? 'Resume' : 'Pause';
    pauseButton.classList.toggle('active', paused);
    pauseButton.setAttribute('aria-pressed', String(paused));
    pauseButton.setAttribute('aria-label', paused ? 'Resume request log' : 'Pause request log');
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

function setFilter(text) {
    filterText = text.trim().toLowerCase();
    filterParsed = filterTerms(filterText);
    renderTable();
}
filterInput.addEventListener('input', () => setFilter(filterInput.value));

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
    dropMissingSelection();
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
        `${requestHost(http.request, multiPort, !!http.callback)}${http.request.url}`,
        String(http.response.status),
        http.response.statusText || '',
        eventBadge(http)?.text || '',
        http.response.fault ? `fault ${http.response.fault}` : '',
    ].join(' ').toLowerCase();
    return matchesFilter(filterParsed, http, hay);
}

function renderTable() {
    const focusWasInTable = requestTable.contains(document.activeElement);
    const visible = eventOrder
        .map((id) => eventsById.get(id))
        .filter(Boolean)
        .filter(eventMatchesFilter);

    const visibleIDs = new Set(visible.map(event => event.id));
    dropMissingSelection(id => visibleIDs.has(id));
    if (visible.length === 0) {
        renderEmptyState(filterText ? 'No matching requests' : 'No requests yet');
        return;
    }

    const existing = new Map(getDataRows().map(row => [Number(row.dataset.id), row]));
    for (const row of requestTableBody.querySelectorAll('tr')) {
        if (!row.dataset.id || !visibleIDs.has(Number(row.dataset.id))) row.remove();
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
        if (selectedRequestId(selection) === http.id) {
            row.classList.add('selected');
        }

        const c0 = row.insertCell(0);
        c0.className = 'time-cell';
        c0.textContent = http.request.time || '';

        const c1 = row.insertCell(1);
        const statusSpan = document.createElement('span');
        const statusText = http.response.statusText || '';
        // A reset fault sends no response, so there is no status to show.
        // A failed callback got no response either.
        statusSpan.textContent = http.response.fault === 'reset' ? 'RST' : http.callback?.error ? 'ERR' : `${http.response.status}`;
        statusSpan.className = 'status-code';
        if (statusText) {
            statusSpan.title = statusText;
            statusSpan.setAttribute('aria-label', `${http.response.status} ${statusText}`);
        }
        const status = parseInt(http.response.status, 10);
        if (status >= 200 && status < 300) statusSpan.classList.add('status-2xx');
        else if (status >= 300 && status < 400) statusSpan.classList.add('status-3xx');
        else if (status >= 400 && status < 500) statusSpan.classList.add('status-4xx');
        else if (status >= 500 || http.response.fault === 'reset' || http.callback?.error) statusSpan.classList.add('status-5xx');
        c1.appendChild(statusSpan);

        const c2 = row.insertCell(2);
        c2.className = 'request-cell';
        const methodSpan = document.createElement('span');
        methodSpan.textContent = http.request.method;
        methodSpan.className = `method-${(http.request.method || '').toLowerCase()}`;
        c2.appendChild(methodSpan);
        const host = requestHost(http.request, multiPort, !!http.callback);
        const target = `${host}${http.request.url}`;
        if (host) {
            const hostSpan = document.createElement('span');
            hostSpan.className = 'request-host';
            hostSpan.textContent = ` ${host}`;
            c2.appendChild(hostSpan);
        }
        const urlSpan = document.createElement('span');
        urlSpan.textContent = host ? http.request.url : ` ${http.request.url}`;
        urlSpan.title = target;
        row.title = `${http.request.method} ${target} · ${http.response.time}`;
        c2.appendChild(urlSpan);
        const badge = eventBadge(http);
        if (badge) {
            const badgeSpan = document.createElement('span');
            badgeSpan.className = 'proxied-badge';
            badgeSpan.textContent = badge.text;
            badgeSpan.title = badge.title;
            c2.appendChild(badgeSpan);
            row.title += ` · ${badge.title}`;
        }
    }

    const focusTarget = syncRowTabindex();
    if (focusWasInTable && focusTarget) {
        focusTarget.focus({ preventScroll: true });
    }
}

function updateDetail(element, newContent) {
    element.textContent = newContent;
    if (!newContent && selection.kind === 'none') resetInspector();
}

function showInspector() {
    workspace.classList.add('show-inspector');
    copyNotice.textContent = '';
    copyGeneration++;
}

// On narrow screens the inspector replaces the list, so move focus to its heading.
function focusInspectorOnMobile() {
    if (window.matchMedia('(max-width: 900px)').matches) (selection.kind === 'route' ? routeTitle : requestTitle).focus();
}

backToList.addEventListener('click', () => {
    workspace.classList.remove('show-inspector');
    if (returnFocus?.isConnected) returnFocus.focus();
    else filterInput.focus();
});

function resetInspector() {
    copyGeneration++;
    matchSummary.textContent = '';
    copyCurlButton.disabled = true;
    copyRouteDraftButton.hidden = true;
    mismatchPanel.hidden = true;
    copyNotice.textContent = '';
    for (const kind of ['request', 'response']) {
        document.getElementById(`${kind}BodyNotice`).textContent = '';
        document.getElementById(`copy${kind === 'request' ? 'Request' : 'Response'}Body`).disabled = true;
    }
}

function routeCurl(route) {
    const port = routePort(route);
    return port ? buildRouteCurl(route, mockOrigin(port)) : '';
}

function renderInspector() {
    exchangeView.hidden = selection.kind === 'route';
    routeView.hidden = selection.kind !== 'route';
    if (selection.kind === 'route') {
        const { route, stale } = selection;
        routeTitle.textContent = route.name || 'Route';
        routeSummary.textContent = stale
            ? 'This route belongs to a previous revision. Select a current route to inspect it.'
            : `Configured route · revision ${route.revision} · ${route.source}:${route.line}`;
        routeDetails.textContent = formatRoute(route, routeCurl(route));
        copyRouteCurlButton.disabled = !routePort(route);
        return;
    }
    const event = eventsById.get(selectedRequestId(selection));
    if (!event) { resetInspector(); return; }
    const match = event.match;
    const route = match?.route;
    matchSummary.textContent = event.callback
        ? `Callback ${eventBadge(event).title.replace(/^Sent/, 'sent')} · ${event.response.time}`
        : route
        ? `${route.name} · ${route.source}:${route.line} · revision ${match.revision} · ${event.response.time}`
        : `${match?.upstream ? `Proxied to ${match.upstream} · no` : 'No'} matching route · revision ${match?.revision ?? '?'} · ${event.response.time}`;
    mismatchPanel.hidden = !!route;
    copyRouteDraftButton.hidden = !!event.callback;
    copyRouteDraftButton.title = route
        ? `Copy a draft .http section for a more specific variant of ${route.name}`
        : 'Copy a draft .http section that would answer this request';
    mismatchDetails.replaceChildren();
    if (!route) {
        const candidates = match?.candidates || [];
        if (!candidates.length) mismatchDetails.textContent = 'No routes are configured for this request.';
        for (const candidate of candidates) {
            const heading = document.createElement('p');
            heading.textContent = `${candidate.method} ${candidate.port ? `:${candidate.port}` : ''}${candidate.path} — ${candidate.name} (${candidate.source}:${candidate.line})`;
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
        const body = eventsById.get(selectedRequestId(selection))?.[kind]?.body;
        if (body) copyText(body.text);
    });
}
copyCurlButton.addEventListener('click', () => {
    try { copyText(buildCurl(eventsById.get(selectedRequestId(selection))?.request)); }
    catch (error) { copyNotice.textContent = error.message; }
});
copyRouteDraftButton.addEventListener('click', () => {
    try { copyText(buildRouteDraft(eventsById.get(selectedRequestId(selection)), { mockPort: MOCK_CONFIG.mockPort })); }
    catch (error) { copyNotice.textContent = error.message; }
});
copyUnmatchedButton.addEventListener('click', async () => {
    const events = [...eventOrder].reverse().map(id => eventsById.get(id)).filter(Boolean);
    const { text, count } = buildRouteDrafts(events, { mockPort: MOCK_CONFIG.mockPort });
    clearError.hidden = true;
    if (!count) {
        clearError.textContent = 'No unmatched requests in the log.';
        clearError.hidden = false;
        return;
    }
    try {
        if (!navigator.clipboard?.writeText) throw new Error('Clipboard unavailable.');
        await navigator.clipboard.writeText(text);
        copyUnmatchedButton.textContent = `Copied ${count} route${count === 1 ? '' : 's'}`;
        setTimeout(() => { copyUnmatchedButton.textContent = 'Copy unmatched as routes'; }, 2000);
    } catch (error) {
        clearError.textContent = error.message || 'Copy failed.';
        clearError.hidden = false;
    }
});
copyRouteCurlButton.addEventListener('click', () => {
    const route = selectedRoute(selection);
    const command = route && routeCurl(route);
    if (command) copyText(command);
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
        if (selection.kind === 'route') {
            selection = afterRoutesLoaded(selection, routes);
            renderInspector();
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
        // Label routes with their port only when they are served on more than one.
        for (const route of routes) {
            const li = document.createElement('li');
            const method = document.createElement('span');
            method.className = `method-${(route.method || '').toLowerCase()}`;
            method.textContent = route.method;
            const path = document.createElement('span');
            path.className = 'route-path';
            const routePath = `${multiPort && route.port ? `:${route.port}` : ''}${route.path}${route.query ? '?' + route.query : ''}`;
            path.textContent = routePath;
            path.title = routePath;
            const name = document.createElement('span');
            name.className = 'route-name';
            name.textContent = route.name || '';
            if (route.name) {
                name.title = route.name;
            }
            // The hit count is its own button: it filters the traffic to the
            // requests this route answered (again to clear the filter).
            const hits = document.createElement('button');
            hits.type = 'button';
            hits.className = 'route-hits';
            hits.classList.toggle('unused', !route.hits);
            hits.textContent = String(route.hits || 0);
            hits.title = `Matched ${hitsLabel(route.hits)}. Show only these requests`;
            hits.setAttribute('aria-label', `Show the requests ${route.name} answered (${hitsLabel(route.hits)})`);
            hits.addEventListener('click', () => {
                const text = routeFilter(route.name);
                filterInput.value = filterInput.value.trim() === text ? '' : text;
                setFilter(filterInput.value);
            });
            const button = document.createElement('button');
            button.type = 'button';
            button.className = 'route-button';
            button.setAttribute('aria-label', `${route.method} ${routePath}: ${route.name}, matched ${hitsLabel(route.hits)}`);
            button.setAttribute('aria-pressed', String(selectedRoute(selection)?.id === route.id));
            button.append(method, path, name);
            button.addEventListener('click', () => {
                selection = selectRoute(route);
                returnFocus = button;
                for (const item of routesList.querySelectorAll('.route-button')) item.setAttribute('aria-pressed', String(item === button));
                for (const row of getDataRows()) row.classList.remove('selected');
                syncRowTabindex();
                showInspector();
                renderInspector();
                focusInspectorOnMobile();
            });
            li.append(button, hits);
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
