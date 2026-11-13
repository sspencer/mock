import { formatMessage, bodyNotice, buildCurl, buildRouteCurl, formatRoute, hitsLabel, methodClass } from './inspector.mjs';
import { buildRouteDraft, buildRouteDrafts } from './draft.mjs';
import { resendDraft, buildResend } from './resend.mjs';
import { attachSplitter, alignedTrafficHeight } from './layout.mjs';
import { buildHAR } from './har.mjs';
import { filterTerms, matchesFilter, routeFilter } from './filter.mjs';
import { candidateRoute, failedLabel, reasonParts } from './nearmiss.mjs';
import { GROUPINGS, groupRoutes, routeCountLabel, routeMatchesSearch } from './routelist.mjs';
import { TrafficStream, requestHost, eventBadge } from './traffic.mjs';
import { noSelection, selectRequest, selectRoute, selectCompare, selectedRequestId, selectedRoute, afterEventsChanged, afterRoutesLoaded } from './selection.mjs';
import { compareEvents, differenceCount } from './compare.mjs';
import { countNodes, editableFixture, formatFixture, losslessIssue, maxTreeNodes, parseFixture } from './fixture.mjs';
import { renderFixtureTree } from './fixturetree.mjs';
import { switchBadge, switchFields, switchPayload } from './switches.mjs';
import { parseRouteSheet, renderRouteSheet } from './routesheet.mjs';
import { highlightMessage, tokenClass } from './highlight.mjs';

const MOCK_CONFIG = JSON.parse(document.getElementById("mock-config").textContent);
const MAX_EVENTS = MOCK_CONFIG.logSize || 1000;
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
const routeSearch = document.getElementById('routeSearch');
const routeGroup = document.getElementById('routeGroup');
const streamStatus = document.getElementById('streamStatus');
const workspace = document.getElementById('workspace');
const requestTitle = document.getElementById('requestTitle');
const routeTitle = document.getElementById('routeTitle');
const routeSummary = document.getElementById('routeSummary');
const copyRouteCurlButton = document.getElementById('copyRouteCurlButton');
const switchButton = document.getElementById('switchButton');
const switchForm = document.getElementById('switchForm');
const switchOff = document.getElementById('switchOff');
const switchStatus = document.getElementById('switchStatus');
const switchDelay = document.getElementById('switchDelay');
const switchFault = document.getElementById('switchFault');
const switchShare = document.getElementById('switchShare');
const switchClear = document.getElementById('switchClear');
const switchCancel = document.getElementById('switchCancel');
const switchStatusText = document.getElementById('switchStatusText');
const matchSummary = document.getElementById('matchSummary');
const exchangeView = document.getElementById('exchangeView');
const routeView = document.getElementById('routeView');
const compareView = document.getElementById('compareView');
const compareTitle = document.getElementById('compareTitle');
const compareSummary = document.getElementById('compareSummary');
const compareDetails = document.getElementById('compareDetails');
const compareButton = document.getElementById('compareButton');
const compareOnlyDiffs = document.getElementById('compareOnlyDiffs');
const routeDetails = document.getElementById('routeDetails');
const editFixtureButton = document.getElementById('editFixtureButton');
const fixtureEditor = document.getElementById('fixtureEditor');
const fixtureFile = document.getElementById('fixtureFile');
const fixtureTreeTab = document.getElementById('fixtureTreeTab');
const fixtureJsonTab = document.getElementById('fixtureJsonTab');
const fixtureFormat = document.getElementById('fixtureFormat');
const fixtureRevert = document.getElementById('fixtureRevert');
const fixtureSave = document.getElementById('fixtureSave');
const fixtureClose = document.getElementById('fixtureClose');
const fixtureStatus = document.getElementById('fixtureStatus');
const fixtureTree = document.getElementById('fixtureTree');
const fixtureText = document.getElementById('fixtureText');
const copyNotice = document.getElementById('copyNotice');
const copyCurlButton = document.getElementById('copyCurlButton');
if (MOCK_CONFIG.redact) {
    // -R masked credentials before they reached the log, so copies and exports carry no secrets.
    for (const button of [exportButton, copyCurlButton]) button.title = 'Credentials are redacted (-R)';
}
const copyRouteDraftButton = document.getElementById('copyRouteDraftButton');
const resendButton = document.getElementById('resendButton');
const resendForm = document.getElementById('resendForm');
const resendMethod = document.getElementById('resendMethod');
const resendUrl = document.getElementById('resendUrl');
const resendHeaders = document.getElementById('resendHeaders');
const resendBody = document.getElementById('resendBody');
const resendStatus = document.getElementById('resendStatus');
const resendSend = document.getElementById('resendSend');
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
// resending is the open resend editor: { id, draft } for the event it edits.
let resending = null;
// pendingSelectId is a resent request to select once its event is shown.
let pendingSelectId = null;
// compareBase is the request picked as side A of a comparison.
let compareBase = null;
let compareDifferencesOnly = true;
// fixtureEdit is the fixture being edited (see openFixture). It outlives the
// editor being closed or another route being selected, so a draft is kept
// until another fixture is opened.
let fixtureEdit = null;

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
routeGroup.value = GROUPINGS.includes(preferences.get('routeGroup')) ? preferences.get('routeGroup') : '';
routeSearch.addEventListener('input', renderRoutes);
routeSearch.addEventListener('keydown', event => {
    if (event.key === 'Escape' && routeSearch.value) {
        event.stopPropagation();
        routeSearch.value = '';
        renderRoutes();
    }
});
routeGroup.addEventListener('change', () => {
    preferences.set('routeGroup', routeGroup.value);
    renderRoutes();
});

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
    if (compareBase != null && !eventsById.has(compareBase)) compareBase = null;
    const previous = selection;
    const next = afterEventsChanged(selection, isDisplayed, (id) => eventsById.has(id));
    if (next === selection) return;
    selection = next;
    if (previous.kind === 'compare') { renderInspector(); return; }
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
let configuredRoutes = [];

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
    requestCount.textContent = `${eventOrder.length} of ${MAX_EVENTS}`;
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
        renderEmptyState(filterText ? 'No requests match this filter.' : '');
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
        // A request no route answered is drawn as a hollow stop on the line.
        if (!http.match?.route && !http.callback) row.classList.add('no-route');

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
        row.title = `${http.request.method} ${target} in ${http.response.time}`;
        c2.appendChild(urlSpan);
        const badge = eventBadge(http);
        if (badge) {
            const badgeSpan = document.createElement('span');
            badgeSpan.className = 'proxied-badge';
            badgeSpan.textContent = badge.text;
            badgeSpan.title = badge.title;
            c2.appendChild(badgeSpan);
            row.title += `. ${badge.title}`;
        }
    }

    const focusTarget = syncRowTabindex();
    if (focusWasInTable && focusTarget) {
        focusTarget.focus({ preventScroll: true });
    }
    selectPendingResend(visibleIDs);
}

// selectPendingResend selects a resent request once its row exists. A
// request the filter hides is not waited for.
function selectPendingResend(visibleIDs) {
    if (pendingSelectId == null || !eventsById.has(pendingSelectId)) return;
    const id = pendingSelectId;
    pendingSelectId = null;
    if (!visibleIDs.has(id)) return;
    const row = getDataRows().find(r => Number(r.dataset.id) === id);
    if (row) selectRow(row, eventsById.get(id));
}

// setDetailText shows a request or response as colored spans. The text is
// set first, so the element reads the same when there is nothing to color.
function setDetailText(element, text) {
    element.textContent = text;
    const tokens = highlightMessage(text);
    if (!tokens.some(token => token.kind)) return;
    element.replaceChildren(...tokens.map(token => {
        const span = document.createElement('span');
        if (token.kind) span.className = tokenClass(token.kind);
        span.textContent = token.text;
        return span;
    }));
}

function updateDetail(element, newContent) {
    setDetailText(element, newContent);
    if (!newContent && selection.kind === 'none') resetInspector();
}

function showInspector() {
    workspace.classList.add('show-inspector');
    copyNotice.textContent = '';
    copyGeneration++;
}

// On narrow screens the inspector replaces the list, so move focus to its heading.
function focusInspectorOnMobile() {
    if (window.matchMedia('(max-width: 900px)').matches) ({ route: routeTitle, compare: compareTitle }[selection.kind] || requestTitle).focus();
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
    resendButton.hidden = true;
    compareButton.hidden = true;
    closeResend();
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
    exchangeView.hidden = selection.kind === 'route' || selection.kind === 'compare';
    routeView.hidden = selection.kind !== 'route';
    compareView.hidden = selection.kind !== 'compare';
    if (selection.kind === 'compare') {
        renderComparison();
        return;
    }
    if (selection.kind === 'route') {
        const { route, stale } = selection;
        routeTitle.textContent = route.name || 'Route';
        routeSummary.textContent = stale
            ? 'This route belongs to a previous revision. Select a current route to inspect it.'
            : `${route.source}:${route.line}, revision ${route.revision}`;
        // The text is the sheet's source (and what a DOM without layout
        // shows); the sheet's elements replace it.
        const routeText = formatRoute(route, routeCurl(route));
        routeDetails.textContent = routeText;
        routeDetails.replaceChildren(...renderRouteSheet(parseRouteSheet(routeText)));
        copyRouteCurlButton.disabled = !routePort(route);
        const editing = isEditingFixture(route);
        const switching = isSwitchOpen(route) && !stale;
        switchButton.disabled = stale;
        switchButton.setAttribute('aria-expanded', String(switching));
        switchForm.hidden = !switching;
        editFixtureButton.hidden = !route.fixture || !editableFixture(route.fixture);
        editFixtureButton.disabled = stale && !editing;
        editFixtureButton.setAttribute('aria-pressed', String(editing));
        fixtureEditor.hidden = !editing;
        routeDetails.hidden = editing;
        return;
    }
    const event = eventsById.get(selectedRequestId(selection));
    if (!event) { resetInspector(); return; }
    const match = event.match;
    const route = match?.route;
    matchSummary.textContent = event.callback
        ? `Callback ${eventBadge(event).title.replace(/^Sent/, 'sent')}, in ${event.response.time}`
        : route
        ? `Answered by ${route.name} (${route.source}:${route.line}, revision ${match.revision}) in ${event.response.time}`
        : `No route matched (revision ${match?.revision ?? '?'})${match?.upstream ? `; forwarded to ${match.upstream}` : ''}, in ${event.response.time}`;
    mismatchPanel.hidden = !!route;
    copyRouteDraftButton.hidden = !!event.callback;
    resendButton.hidden = !!event.callback;
    updateCompareButton(event);
    if (resending && resending.id !== event.id) closeResend();
    copyRouteDraftButton.title = route
        ? `Copy a draft .http section for a more specific variant of ${route.name}`
        : 'Copy a draft .http section that would answer this request';
    mismatchDetails.replaceChildren();
    if (!route) {
        const candidates = match?.candidates || [];
        if (!candidates.length) mismatchDetails.textContent = 'No routes are configured for this request.';
        for (const candidate of candidates) mismatchDetails.append(nearMissCard(candidate));
        // Open the explanation for unmatched requests; it is why you look.
        // Only when a new event is shown, so closing it sticks across refreshes.
        if (mismatchPanel.dataset.event !== String(event.id)) mismatchPanel.open = candidates.length > 0;
        mismatchPanel.dataset.event = String(event.id);
    }
    copyCurlButton.disabled = false;
    for (const kind of ['request', 'response']) {
        const message = event[kind];
        setDetailText(document.getElementById(`${kind}Details`), formatMessage(message, pretty[kind], kind === 'request'));
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
// The Compare button picks side A, then compares A with the request shown.
function updateCompareButton(event) {
    compareButton.hidden = false;
    if (compareBase == null) {
        compareButton.textContent = 'Compare…';
        compareButton.title = 'Pick this request as the first side of a comparison, then select another request';
    } else if (compareBase === event.id) {
        compareButton.textContent = 'Cancel compare';
        compareButton.title = 'This request is picked for comparison; select another request to compare with it';
    } else {
        compareButton.textContent = `Compare with #${compareBase}`;
        compareButton.title = `Show request #${compareBase} and this request side by side`;
    }
}

compareButton.addEventListener('click', () => {
    const id = selectedRequestId(selection);
    if (id == null) return;
    if (compareBase == null) {
        compareBase = id;
        copyNotice.textContent = `Request #${id} is picked. Select another request and choose Compare with #${id}.`;
    } else if (compareBase === id) {
        compareBase = null;
        copyNotice.textContent = '';
    } else {
        selection = selectCompare(compareBase, id);
        compareBase = null;
        copyNotice.textContent = '';
        renderInspector();
        focusInspectorOnMobile();
        return;
    }
    updateCompareButton(eventsById.get(id));
});

compareOnlyDiffs.addEventListener('click', () => {
    compareDifferencesOnly = !compareDifferencesOnly;
    renderComparison();
});

document.getElementById('compareSwap').addEventListener('click', () => {
    if (selection.kind !== 'compare') return;
    selection = selectCompare(selection.b, selection.a);
    renderComparison();
});

document.getElementById('compareClose').addEventListener('click', () => {
    if (selection.kind !== 'compare') return;
    const id = selection.b;
    const row = getDataRows().find(r => Number(r.dataset.id) === id);
    if (row) { selectRow(row, eventsById.get(id)); return; }
    selection = selectRequest(id);
    renderInspector();
});

const valueText = (values) => values == null ? '—' : values.join('\n');

// compareTable renders field rows as a three-column table. Unchanged rows
// are left out when only differences are shown.
function compareTable(title, rows, labelA, labelB) {
    const shown = compareDifferencesOnly ? rows.filter(row => !row.same) : rows;
    const section = document.createElement('section');
    section.className = 'compare-section';
    const heading = document.createElement('h3');
    heading.textContent = title;
    section.append(heading);
    if (!shown.length) {
        const empty = document.createElement('p');
        empty.className = 'compare-empty';
        empty.textContent = rows.length ? 'No differences.' : 'None.';
        section.append(empty);
        return section;
    }
    const table = document.createElement('table');
    table.className = 'compare-table';
    const head = table.insertRow();
    for (const text of ['', labelA, labelB]) {
        const th = document.createElement('th');
        th.scope = 'col';
        th.textContent = text;
        head.appendChild(th);
    }
    for (const row of shown) {
        const tr = table.insertRow();
        tr.className = row.same ? 'same' : row.a == null ? 'added' : row.b == null ? 'removed' : 'changed';
        const name = document.createElement('th');
        name.scope = 'row';
        name.textContent = row.name;
        tr.appendChild(name);
        tr.insertCell().textContent = valueText(row.a);
        tr.insertCell().textContent = valueText(row.b);
    }
    section.append(table);
    return section;
}

// compareBody renders a body comparison as a unified line diff.
function compareBody(title, body) {
    const section = document.createElement('section');
    section.className = 'compare-section';
    const heading = document.createElement('h3');
    heading.textContent = title;
    section.append(heading);
    if (body.note) {
        const note = document.createElement('p');
        note.className = 'compare-empty';
        note.textContent = body.note;
        section.append(note);
    }
    if (!body.lines?.length || (body.same && compareDifferencesOnly)) {
        if (!body.note) {
            const same = document.createElement('p');
            same.className = 'compare-empty';
            same.textContent = 'No differences.';
            section.append(same);
        }
        return section;
    }
    const pre = document.createElement('pre');
    pre.className = 'detail-content compare-diff';
    for (const line of body.lines) {
        const span = document.createElement('span');
        span.className = line.op === '-' ? 'diff-removed' : line.op === '+' ? 'diff-added' : 'diff-same';
        span.textContent = `${line.op} ${line.text}\n`;
        pre.appendChild(span);
    }
    section.append(pre);
    return section;
}

function renderComparison() {
    if (selection.kind !== 'compare') return;
    const a = eventsById.get(selection.a), b = eventsById.get(selection.b);
    if (!a || !b) return;
    const labelA = `A: #${a.id}`, labelB = `B: #${b.id}`;
    const comparison = compareEvents(a, b);
    const count = differenceCount(comparison);
    compareTitle.textContent = `Compare #${a.id} and #${b.id}`;
    compareSummary.textContent = `A is ${a.request.method} ${a.request.url}, B is ${b.request.method} ${b.request.url}. ${count ? `${count} difference${count === 1 ? '' : 's'}` : 'No differences besides timing'}.`;
    compareOnlyDiffs.setAttribute('aria-pressed', String(compareDifferencesOnly));
    compareDetails.replaceChildren(
        compareTable('Overview', comparison.overview, labelA, labelB),
        compareTable('Query parameters', comparison.query, labelA, labelB),
        compareTable('Request headers', comparison.requestHeaders, labelA, labelB),
        compareBody('Request body', comparison.requestBody),
        compareTable('Response headers', comparison.responseHeaders, labelA, labelB),
        compareBody('Response body', comparison.responseBody),
    );
}

function closeResend() {
    resending = null;
    resendForm.hidden = true;
    resendButton.setAttribute('aria-expanded', 'false');
    resendStatus.textContent = '';
}

resendButton.addEventListener('click', () => {
    if (resending) { closeResend(); return; }
    const event = eventsById.get(selectedRequestId(selection));
    const draft = resendDraft(event);
    if (draft.error) { copyNotice.textContent = draft.error; return; }
    resending = { id: event.id, draft };
    resendMethod.value = draft.method;
    resendUrl.value = draft.url;
    resendHeaders.value = draft.headers;
    resendBody.value = draft.bodyEditable ? draft.body : '';
    resendBody.disabled = !draft.bodyEditable;
    resendBody.placeholder = draft.bodyEditable ? 'No body' : 'The binary body is resent unchanged.';
    resendStatus.textContent = draft.host ? `Sent to ${draft.host} through the admin server.` : '';
    resendForm.hidden = false;
    resendButton.setAttribute('aria-expanded', 'true');
    resendMethod.focus();
});

document.getElementById('resendCancel').addEventListener('click', closeResend);

resendForm.addEventListener('submit', async (submit) => {
    submit.preventDefault();
    if (!resending) return;
    let payload;
    try {
        payload = buildResend(resending.draft, { method: resendMethod.value, url: resendUrl.value, headers: resendHeaders.value, body: resendBody.value }, resending.id);
    } catch (error) {
        resendStatus.textContent = error.message;
        return;
    }
    resendSend.disabled = true;
    resendStatus.textContent = 'Sending…';
    try {
        const res = await fetch('resend', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'XMLHttpRequest' },
            body: JSON.stringify(payload),
        });
        if (!res.ok) throw new Error((await res.text()).trim() || `Resend failed with HTTP ${res.status}.`);
        const { id } = await res.json();
        closeResend();
        pendingSelectId = id;
        scheduleRender();
    } catch (error) {
        resendStatus.textContent = error.message || 'Resend failed.';
    } finally {
        resendSend.disabled = false;
    }
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
// ---------- Route switches ----------
//
// switchOpenFor names the route whose switches form is open (by place, since
// a reload changes route ids); the form follows it while it stays selected.
let switchOpenFor = null;
const routePlace = (route) => `${route.source}\n${route.line}\n${route.name}`;
const isSwitchOpen = (route) => switchOpenFor === routePlace(route);

function fillSwitchForm(route) {
    const fields = switchFields(route);
    switchOff.checked = fields.off;
    switchStatus.value = fields.status;
    switchDelay.value = fields.delay;
    switchFault.value = fields.fault;
    switchShare.value = fields.share;
    switchStatusText.textContent = route.override && !route.switch ? `A switch for all routes applies: ${route.override}.` : '';
}

async function sendSwitch(method, url, payload) {
    const res = await fetch(url, {
        method,
        headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'XMLHttpRequest' },
        body: payload ? JSON.stringify(payload) : undefined,
        signal: AbortSignal.timeout(5000),
    });
    if (!res.ok) throw new Error((await res.text()).trim() || `HTTP ${res.status}`);
    lastRoutesJSON = null;
    await loadRoutes();
    loadState();
}

switchButton.addEventListener('click', () => {
    const route = selectedRoute(selection);
    if (!route) return;
    if (isSwitchOpen(route)) {
        switchOpenFor = null;
    } else {
        switchOpenFor = routePlace(route);
        fillSwitchForm(route);
    }
    renderInspector();
    if (switchOpenFor) switchOff.focus();
});
switchCancel.addEventListener('click', () => { switchOpenFor = null; renderInspector(); switchButton.focus(); });
switchForm.addEventListener('submit', async (event) => {
    event.preventDefault();
    const route = selectedRoute(selection);
    if (!route) return;
    const { payload, error } = switchPayload(route, {
        off: switchOff.checked, status: switchStatus.value, delay: switchDelay.value, fault: switchFault.value, share: switchShare.value,
    });
    if (error) { switchStatusText.textContent = error; return; }
    try {
        await sendSwitch('POST', 'faults', payload);
        switchStatusText.textContent = 'Switched. It lasts until you switch it back on or POST /reset.';
    } catch (err) {
        switchStatusText.textContent = `Not switched: ${err.message}`;
    }
});
switchClear.addEventListener('click', async () => {
    const route = selectedRoute(selection);
    if (!route) return;
    try {
        await sendSwitch('DELETE', `faults?route=${encodeURIComponent(route.id)}`);
        fillSwitchForm(selectedRoute(selection) || route);
        switchStatusText.textContent = 'Switched back on: the route answers as its file says.';
    } catch (err) {
        switchStatusText.textContent = `Not changed: ${err.message}`;
    }
});

copyRouteCurlButton.addEventListener('click', () => {
    const route = selectedRoute(selection);
    const command = route && routeCurl(route);
    if (command) copyText(command);
});

// ---------- Fixture editor ----------
//
// fixtureEdit is { key, file, fixture, version, mode: 'tree' | 'json', value,
// text, textStale, dirty, open, saving, tooLarge, toggled }. In tree mode the
// value is the source of truth; in JSON mode the text is. textStale marks
// text that no longer reflects value (a tree edit was made), so the original
// formatting is kept until the tree changes something.

const fixtureKey = (route) => `${route.source}\n${route.fixture}`;
const isEditingFixture = (route) => !!(route.fixture && fixtureEdit?.open && fixtureEdit.key === fixtureKey(route));

// treeRefusal explains why a fixture stays in the JSON view.
const treeRefusal = (issue) => `${issue} The tree view would rewrite that, so edit this file as JSON; it is saved as written.`;

function setFixtureStatus(message, kind = '') {
    fixtureStatus.textContent = message;
    fixtureStatus.classList.toggle('error', kind === 'error');
    fixtureStatus.classList.toggle('ok', kind === 'ok');
}

async function openFixture(route) {
    if (fixtureEdit?.key === fixtureKey(route)) {
        fixtureEdit.open = true;
        renderInspector();
        renderFixtureEditor();
        return;
    }
    await loadFixture(route);
}

async function loadFixture(route) {
    const key = fixtureKey(route);
    try {
        const res = await fetch(`fixture?route=${encodeURIComponent(route.id)}`, { signal: AbortSignal.timeout(5000) });
        if (!res.ok) throw new Error((await res.text()).trim() || `status ${res.status}`);
        const file = await res.json();
        const edit = { key, file: file.file, fixture: file.fixture, version: file.version, mode: 'tree', value: null, text: file.content, textStale: false, dirty: false, open: true, saving: false, tooLarge: false, toggled: new Set() };
        let status = ['', ''];
        if (!file.version) {
            edit.value = {};
            edit.text = formatFixture({});
            status = ['The file does not exist yet; Save creates it.', ''];
        } else {
            const parsed = parseFixture(file.content);
            if (parsed.error) {
                edit.mode = 'json';
                status = [`${parsed.error}. Fix it in the JSON view.`, 'error'];
            } else if (countNodes(parsed.value) > maxTreeNodes) {
                edit.mode = 'json';
                edit.tooLarge = true;
                status = [`This fixture has more than ${maxTreeNodes} values; edit it as JSON.`, ''];
            } else if (losslessIssue(file.content)) {
                edit.mode = 'json';
                status = [treeRefusal(losslessIssue(file.content)), ''];
            } else {
                edit.value = parsed.value;
            }
        }
        // The editor and status always follow the newest load, even when
        // another route was selected meanwhile, so going back to this route
        // never shows a previous fixture's status.
        fixtureEdit = edit;
        setFixtureStatus(...status);
        renderFixtureEditor();
        if (isEditingFixture(selectedRoute(selection) || {})) renderInspector();
    } catch (error) {
        copyNotice.textContent = `Could not open the fixture: ${error.message}`;
    }
}

function renderFixtureEditor() {
    const edit = fixtureEdit;
    if (!edit) return;
    const tree = edit.mode === 'tree';
    fixtureFile.textContent = edit.fixture;
    fixtureFile.title = edit.file;
    fixtureTreeTab.setAttribute('aria-pressed', String(tree));
    fixtureJsonTab.setAttribute('aria-pressed', String(!tree));
    fixtureTreeTab.disabled = edit.tooLarge;
    fixtureTree.hidden = !tree;
    fixtureText.hidden = tree;
    fixtureFormat.disabled = tree;
    fixtureSave.disabled = edit.saving;
    if (tree) {
        renderFixtureTree(fixtureTree, edit.value, {
            toggled: edit.toggled,
            onChange: changeFixtureValue,
            onError: (message) => setFixtureStatus(message, 'error'),
        });
    } else if (fixtureText.value !== edit.text) {
        fixtureText.value = edit.text;
    }
}

function markFixtureDirty() {
    fixtureEdit.dirty = true;
    setFixtureStatus('Unsaved changes.');
}

function changeFixtureValue(value) {
    if (value !== fixtureEdit.value) {
        fixtureEdit.value = value;
        fixtureEdit.textStale = true;
        markFixtureDirty();
    }
    renderFixtureEditor();
}

function setFixtureMode(mode) {
    const edit = fixtureEdit;
    if (!edit || edit.mode === mode) return;
    if (mode === 'json') {
        if (edit.textStale) edit.text = formatFixture(edit.value);
        edit.textStale = false;
    } else {
        const parsed = parseFixture(edit.text);
        if (parsed.error) { setFixtureStatus(parsed.error, 'error'); return; }
        if (countNodes(parsed.value) > maxTreeNodes) {
            setFixtureStatus(`This fixture has more than ${maxTreeNodes} values; edit it as JSON.`);
            return;
        }
        const issue = losslessIssue(edit.text);
        if (issue) { setFixtureStatus(treeRefusal(issue), 'error'); return; }
        edit.value = parsed.value;
        if (fixtureStatus.classList.contains('error')) setFixtureStatus(edit.dirty ? 'Unsaved changes.' : '');
    }
    edit.mode = mode;
    renderFixtureEditor();
}

async function saveFixture() {
    const edit = fixtureEdit;
    if (!edit || edit.saving) return;
    let content = edit.text;
    if (edit.mode === 'tree') {
        if (edit.textStale) content = formatFixture(edit.value);
    } else {
        const parsed = parseFixture(content);
        if (parsed.error) { setFixtureStatus(parsed.error, 'error'); return; }
    }
    edit.saving = true;
    setFixtureStatus('Saving…');
    renderFixtureEditor();
    try {
        const res = await fetch('fixture', {
            method: 'PUT',
            headers: { 'Content-Type': 'application/json', 'X-Requested-With': 'XMLHttpRequest' },
            body: JSON.stringify({ file: edit.file, content, version: edit.version }),
            signal: AbortSignal.timeout(10000),
        });
        if (res.status === 409) {
            const reply = await res.json();
            if (fixtureEdit === edit) setFixtureStatus(`Not saved: ${reply.error}. Your edits are kept; Reload discards them.`, 'error');
            return;
        }
        if (!res.ok) throw new Error((await res.text()).trim() || `status ${res.status}`);
        const reply = await res.json();
        edit.version = reply.version;
        edit.text = content;
        edit.textStale = false;
        edit.dirty = false;
        if (fixtureEdit === edit) setFixtureStatus('Saved. The next response uses it.', 'ok');
    } catch (error) {
        if (fixtureEdit === edit) setFixtureStatus(`Not saved: ${error.message}`, 'error');
    } finally {
        edit.saving = false;
        if (fixtureEdit === edit) renderFixtureEditor();
    }
}

editFixtureButton.addEventListener('click', () => {
    const route = selectedRoute(selection);
    if (!route?.fixture) return;
    if (isEditingFixture(route)) { fixtureEdit.open = false; renderInspector(); return; }
    openFixture(route);
});
fixtureTreeTab.addEventListener('click', () => setFixtureMode('tree'));
fixtureJsonTab.addEventListener('click', () => setFixtureMode('json'));
fixtureText.addEventListener('input', () => {
    if (!fixtureEdit) return;
    fixtureEdit.text = fixtureText.value;
    markFixtureDirty();
});
fixtureFormat.addEventListener('click', () => {
    if (!fixtureEdit) return;
    const parsed = parseFixture(fixtureEdit.text);
    if (parsed.error) { setFixtureStatus(parsed.error, 'error'); return; }
    const text = formatFixture(parsed.value);
    if (text !== fixtureEdit.text) { fixtureEdit.text = text; markFixtureDirty(); }
    renderFixtureEditor();
});
fixtureRevert.addEventListener('click', () => {
    const route = selectedRoute(selection);
    if (!route || !fixtureEdit) return;
    loadFixture(route);
});
fixtureSave.addEventListener('click', saveFixture);
fixtureClose.addEventListener('click', () => {
    if (!fixtureEdit) return;
    fixtureEdit.open = false;
    renderInspector();
    editFixtureButton.focus();
});

function renderEmptyState(message) {
    requestTableBody.replaceChildren();
    const row = requestTableBody.insertRow();
    const cell = row.insertCell();
    cell.colSpan = 3;
    cell.className = 'empty-state';
    cell.textContent = message || 'No requests yet. Send one to the mock port and it shows up here.';
}

// nearMissCard shows one route that nearly answered an unmatched request:
// the route, each failed requirement highlighted, and buttons to inspect the
// route or list the other requests it nearly answered.
function nearMissCard(candidate) {
    const card = document.createElement('section');
    card.className = 'near-miss';
    const heading = document.createElement('p');
    heading.className = 'near-miss-route';
    const method = document.createElement('span');
    method.className = methodClass(candidate.method);
    method.textContent = candidate.method;
    const path = document.createElement('span');
    path.className = 'route-path';
    path.textContent = `${candidate.port ? `:${candidate.port}` : ''}${candidate.path}`;
    const name = document.createElement('span');
    name.className = 'near-miss-name';
    name.textContent = `${candidate.name} (${candidate.source}:${candidate.line})`;
    heading.append(method, path, name);
    const count = document.createElement('p');
    count.className = 'near-miss-count';
    count.textContent = failedLabel(candidate);
    const reasons = document.createElement('ul');
    for (const reason of candidate.reasons || []) {
        const { subject, detail } = reasonParts(reason);
        const item = document.createElement('li');
        if (subject) {
            const mark = document.createElement('mark');
            mark.textContent = subject;
            item.append(mark, ' ');
        }
        item.append(detail);
        reasons.append(item);
    }
    const actions = document.createElement('p');
    actions.className = 'near-miss-actions';
    const route = candidateRoute(configuredRoutes, candidate);
    if (route) {
        const inspect = document.createElement('button');
        inspect.type = 'button';
        inspect.className = 'clear-button';
        inspect.textContent = 'Inspect route';
        inspect.title = `Show ${candidate.name} in the inspector`;
        inspect.addEventListener('click', () => inspectRoute(route));
        actions.append(inspect);
    }
    const similar = document.createElement('button');
    similar.type = 'button';
    similar.className = 'clear-button';
    similar.textContent = 'Similar misses';
    similar.title = `Show the unmatched requests ${candidate.name} nearly answered`;
    similar.addEventListener('click', () => {
        filterInput.value = routeFilter(candidate.name, 'near');
        setFilter(filterInput.value);
    });
    actions.append(similar);
    card.append(heading, count, reasons, actions);
    return card;
}

// inspectRoute selects a configured route, as clicking it in the list does.
function inspectRoute(route) {
    selection = selectRoute(route);
    for (const item of routesList.querySelectorAll('.route-button')) item.setAttribute('aria-pressed', 'false');
    for (const row of getDataRows()) row.classList.remove('selected');
    syncRowTabindex();
    showInspector();
    renderInspector();
    focusInspectorOnMobile();
}

// renderRoutes draws the routes list from configuredRoutes, narrowed by the
// search box and split into groups by the grouping menu.
function renderRoutes() {
    const routes = configuredRoutes;
    const shown = routes.filter(route => routeMatchesSearch(route, routeSearch.value));
    routeCount.textContent = routeCountLabel(shown.length, routes.length);
    routeCount.setAttribute('aria-label', shown.length === routes.length ? `${routes.length} configured routes` : `${shown.length} of ${routes.length} configured routes shown`);
    routesList.replaceChildren();
    if (!shown.length) {
        const li = document.createElement('li');
        li.className = 'empty-state';
        li.textContent = routes.length ? 'No routes match the search' : 'No routes configured';
        routesList.appendChild(li);
        return;
    }
    for (const group of groupRoutes(shown, routeGroup.value)) {
        if (group.label) {
            const heading = document.createElement('li');
            heading.className = 'route-group';
            heading.setAttribute('role', 'presentation');
            heading.textContent = `${group.label} · ${group.routes.length}`;
            routesList.appendChild(heading);
        }
        for (const route of group.routes) routesList.appendChild(routeItem(route));
    }
}

// routeItem is one route's row: the route button and its hit count.
// Routes are labeled with their port only when they are served on more than one.
function routeItem(route) {
    const li = document.createElement('li');
    const method = document.createElement('span');
    method.className = methodClass(route.method);
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
    const tags = (route.tags || []).map(tag => {
        const chip = document.createElement('span');
        chip.className = 'route-tag';
        chip.textContent = tag;
        chip.title = `Tag ${tag}: filter the traffic with tag:${tag}`;
        return chip;
    });
    const badge = switchBadge(route);
    const switched = document.createElement('span');
    if (badge) {
        switched.className = 'route-switch';
        switched.textContent = badge.text;
        switched.title = badge.title;
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
    button.setAttribute('aria-label', `${route.method} ${routePath}: ${route.name}${tags.length ? `, tagged ${route.tags.join(', ')}` : ''}${badge ? `, switched ${badge.text}` : ''}, matched ${hitsLabel(route.hits)}`);
    button.setAttribute('aria-pressed', String(selectedRoute(selection)?.id === route.id));
    button.append(method, path, ...(badge ? [switched] : []), name, ...tags);
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
    return li;
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
        configuredRoutes = routes;
        if (selection.kind === 'route') {
            // Saving a fixture reloads the routes; keep the edited route selected.
            const route = selection.route;
            selection = afterRoutesLoaded(selection, routes, fixtureEdit?.key === fixtureKey(route) ? route : null);
            renderInspector();
        }
        renderRoutes();
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
