import test from 'node:test';
import assert from 'node:assert/strict';
import { sampleFormBody, sampleJSONBody, buildCurl, buildRouteCurl, formatMessage, formatRoute, bodyNotice, sampleParam, describeMatcher, isSampleExact, sampleFor, comparison, jsonPathSegments, sampleMethod, methodClass } from '../static/inspector.mjs';
import { attachSplitter, clampPercent, alignedTrafficHeight } from '../static/layout.mjs';

const message = {
    method: 'POST', url: '/users?q=one', scheme: 'https', host: 'example.test', httpVersion: 'HTTP/1.1',
    headers: { 'Content-Type': ['application/json'], 'Content-Length': ['7'], 'X-Name': ["O'Brien"] },
    body: { text: '{"a":1}', capturedSize: 7, size: 7, truncated: false },
};

test('Pretty formats valid JSON while Raw preserves body bytes', () => {
    assert.match(formatMessage(message, true, true), /\{\n  "a": 1\n\}/);
    assert.ok(formatMessage(message, false, true).endsWith('{"a":1}'));
    const truncated = { ...message, body: { ...message.body, text: '{"a":', truncated: true } };
    assert.ok(formatMessage(truncated, true, true).endsWith('{"a":'));
    assert.match(bodyNotice(truncated.body), /Truncated/);
});

test('Copy cURL quotes shell metacharacters and omits transport-managed headers', () => {
    const command = buildCurl(message);
    assert.ok(command.includes("'X-Name: O'\\''Brien'"));
    assert.ok(command.startsWith("printf '%s' '{\"a\":1}' | curl"));
    assert.ok(command.includes("--data-binary @-"));
    assert.ok(buildCurl({ ...message, body: { ...message.body, text: "@private-file" } }).startsWith("printf '%s' '@private-file' | curl"));
    assert.ok(command.includes("'https://example.test/users?q=one'"));
    assert.ok(!command.includes('Content-Length'));
    assert.throws(() => buildCurl({ ...message, body: { ...message.body, truncated: true } }), /not fully captured/);
    assert.throws(() => buildCurl({ ...message, body: { ...message.body, encoding: 'base64' } }), /Binary/);
});

test('Route view includes source, requirements, file and template', () => {
    const text = formatRoute({ method: 'GET', path: '/x', name: 'Example', source: 'test.http', line: 3, revision: 2, status: 201, delay: '1s', file: 'body.json', matchHeaders: { Authorization: ['*'] }, body: '{"x":1}' });
    for (const value of ['test.http:3', 'Authorization: *', '201', '1s', 'body.json', '{"x":1}', 'Matched: 0 requests']) assert.ok(text.includes(value));
    assert.ok(formatRoute({ method: 'GET', path: '/x', hits: 1 }).includes('Matched: 1 request\n'));
    assert.ok(formatRoute({ method: 'GET', path: '/x', hits: 12 }).includes('Matched: 12 requests'));
});

test('Route view shows delay ranges, streaming and faults', () => {
    const text = formatRoute({ method: 'GET', path: '/x', status: 200, delay: '100ms..800ms', stream: '50ms', fault: 'reset 25%' });
    assert.ok(text.includes('Response: 200 · delay 100ms..800ms · stream 50ms · fault reset 25%'));
    assert.ok(!formatRoute({ method: 'GET', path: '/x', status: 200, delay: '0s' }).includes('stream'));
});

test('Route view shows the rate limit', () => {
    assert.ok(formatRoute({ method: 'GET', path: '/x', rateLimit: '5/1s' }).includes('Rate limit: 5/1s'));
    assert.ok(!formatRoute({ method: 'GET', path: '/x' }).includes('Rate limit'));
});

test('Route cURL targets the mock origin with sample path parameters', () => {
    const origin = 'http://localhost:8080';
    assert.equal(buildRouteCurl({ method: 'GET', path: '/users/:id' }, origin), "curl 'http://localhost:8080/users/1'");
    assert.equal(buildRouteCurl({ method: 'GET', path: '/users/:userId/posts/:slug', query: 'page=2&q=a+b' }, origin),
        "curl 'http://localhost:8080/users/1/posts/example?page=2&q=a+b'");
    assert.equal(buildRouteCurl({ method: 'DELETE', path: '/users/:id' }, origin), "curl --request DELETE 'http://localhost:8080/users/1'");
    assert.equal(buildRouteCurl({ method: 'HEAD', path: '/' }, origin), "curl --head 'http://localhost:8080/'");
    assert.equal(buildRouteCurl({ method: 'POST', path: '/users', matchHeaders: { Authorization: ['*'], 'Content-Type': ["application/json"] } }, origin),
        "curl --request POST --header 'Authorization: example' --header 'Content-Type: application/json' 'http://localhost:8080/users'");
    assert.ok(buildRouteCurl({ method: 'GET', path: '/x' }, 'https://localhost:8443').startsWith('curl --insecure '));
    assert.equal(sampleParam('id'), '1');
    assert.equal(sampleParam('user_id'), '1');
    assert.equal(sampleParam('name'), 'example');
});

test('Route view and cURL handle absent and regexp matchers', () => {
    const route = { method: 'GET', path: '/users/:id/:slug', query: 'a=1', source: 't.http', line: 1, revision: 1, status: 200,
        matchHeaders: { Authorization: ['~Bearer .+'], 'X-Debug': ['!'] },
        matchQuery: { page: ['~[0-9]+'], debug: ['!'], kind: ['cat'] },
        matchPath: { id: '~[0-9]+', slug: 'a b' } };
    assert.equal(buildRouteCurl(route, 'http://localhost:8080'),
        "curl --header 'Authorization: example' 'http://localhost:8080/users/1/a%20b?a=1&page=1&kind=cat'");
    const text = formatRoute(route);
    for (const value of ['Authorization: ~Bearer .+ (regular expression)', 'X-Debug: ! (must be absent)',
        'Query requirements\npage: ~[0-9]+ (regular expression)', 'debug: ! (must be absent)', 'kind: cat',
        'Path parameter requirements\n:id: ~[0-9]+ (regular expression)', ':slug: a b']) {
        assert.ok(text.includes(value), value);
    }
    assert.ok(!formatRoute({ ...route, matchQuery: undefined, matchPath: undefined }).includes('Query requirements'));
});

test('Route view shows the sample request when provided', () => {
    const route = { method: 'GET', path: '/x', source: 't.http', line: 1, revision: 1, status: 200 };
    assert.ok(formatRoute(route, "curl 'http://localhost:8080/x'").includes("Sample request\ncurl 'http://localhost:8080/x'"));
    assert.ok(!formatRoute(route).includes('Sample request'));
});

test('Splitter persists clamped pointer and keyboard changes and restores defaults', () => {
    class Handle extends EventTarget {
        values = {};
        classList = { add() {}, remove() {} };
        setAttribute(k, v) { this.values[k] = v; }
        setPointerCapture() {}
    }
    const handle = new Handle();
    const styles = {}; const saved = {};
    const container = { style: { setProperty: (k, v) => styles[k] = v }, getBoundingClientRect: () => ({ width: 1000, height: 500, left: 100, top: 0 }) };
    attachSplitter(handle, container, { axis: 'x', property: '--width', key: 'width', initial: 45 }, { get: () => 'invalid', set: (k, v) => saved[k] = v });
    const emit = (type, values) => { const event = new Event(type, { cancelable: true }); Object.assign(event, values); handle.dispatchEvent(event); };
    assert.equal(styles['--width'], '45%');
    emit('keydown', { key: 'ArrowRight', shiftKey: true });
    assert.equal(styles['--width'], '55%');
    emit('pointerdown', { button: 0, pointerId: 1 });
    emit('pointermove', { clientX: 1100 });
    assert.equal(saved.width, '80');
    emit('pointerup', {});
    emit('dblclick', {});
    assert.equal(saved.width, '45');
    assert.equal(clampPercent(null, 45), 45);
    assert.equal(clampPercent(-100, 45), 20);
});

test('Traffic height aligns the routes panel with the response pane', () => {
    assert.equal(alignedTrafficHeight(800, 200, 70), 75);
    assert.equal(alignedTrafficHeight(0, 200, 70), 70);
    assert.equal(alignedTrafficHeight(800, 0, 70), 70);
    assert.equal(alignedTrafficHeight(800, undefined, 70), 70);
});

test('Splitter computes its default when initial is a function', () => {
    const handle = new (class extends EventTarget { setAttribute() {} })();
    const styles = {};
    const container = { style: { setProperty: (k, v) => styles[k] = v } };
    let computed = 62;
    attachSplitter(handle, container, { axis: 'y', property: '--h', key: 'h', initial: () => computed, min: 30, max: 85 }, { get: () => null, set() {} });
    assert.equal(styles['--h'], '62%');
    computed = 99;
    handle.dispatchEvent(new Event('dblclick'));
    assert.equal(styles['--h'], '85%');
    attachSplitter(handle, container, { axis: 'y', property: '--h', key: 'h', initial: () => 62 }, { get: () => '40', set() {} });
    assert.equal(styles['--h'], '40%');
});

test('Route cURL sends a sample body for body and JSON matchers', () => {
    const origin = 'http://localhost:8080';
    assert.equal(buildRouteCurl({ method: 'POST', path: '/login', matchJson: { 'user.name': 'alice', 'user.age': '42', 'user.id': '!', 'tags.0': '~a+', active: 'true' } }, origin),
        `curl --request POST --header 'Content-Type: application/json' --data-raw '{"user":{"name":"alice","age":42},"tags":["example"],"active":true}' 'http://localhost:8080/login'`);
    assert.equal(buildRouteCurl({ method: 'POST', path: '/echo', matchBody: 'ping' }, origin), "curl --request POST --data-raw 'ping' 'http://localhost:8080/echo'");
    assert.equal(buildRouteCurl({ method: 'POST', path: '/echo', matchBody: '~.*x.*' }, origin), "curl --request POST 'http://localhost:8080/echo'");
    assert.equal(sampleJSONBody({ a: '1', 'a.b': '2' }), '');
    assert.equal(sampleJSONBody({ x: '!' }), '');
    assert.equal(sampleJSONBody({ 'roles.*': 'admin', 'items.*.sku': '~A[0-9]+' }), '{"roles":["admin"],"items":[{"sku":"example"}]}');
    const text = formatRoute({ method: 'POST', path: '/login', source: 't.http', line: 1, revision: 1, status: 200, matchBody: '~.*x.*', matchJson: { 'user.name': 'alice' } });
    assert.ok(text.includes('Body requirements\nbody: ~.*x.* (regular expression)\njson user.name: alice'), text);
});

test('Route cURL fills wildcards and sends form bodies', () => {
    const origin = 'http://localhost:8080';
    assert.equal(buildRouteCurl({ method: 'GET', path: '/files/*path' }, origin), "curl 'http://localhost:8080/files/example'");
    assert.equal(buildRouteCurl({ method: 'GET', path: '/api/*' }, origin), "curl 'http://localhost:8080/api/example'");
    assert.equal(buildRouteCurl({ method: 'GET', path: '/files/*path', matchPath: { path: 'a b/c.txt' } }, origin), "curl 'http://localhost:8080/files/a%20b/c.txt'");
    assert.equal(buildRouteCurl({ method: 'POST', path: '/token', matchForm: { grant_type: ['client_credentials'], scope: ['~.+'], debug: ['!'] } }, origin),
        "curl --request POST --data-raw 'grant_type=client_credentials&scope=example' 'http://localhost:8080/token'");
    assert.equal(sampleFormBody({}), '');
    const text = formatRoute({ method: 'POST', path: '/token', source: 't.http', line: 1, revision: 1, status: 200, matchForm: { grant_type: ['client_credentials'] } });
    assert.ok(text.includes('Body requirements\nform grant_type: client_credentials'), text);
});

test('describeMatcher explains negated matchers', () => {
    assert.equal(describeMatcher('!'), '! (must be absent)');
    assert.equal(describeMatcher('!guest'), '!guest (present, not guest)');
    assert.equal(describeMatcher('!~test-.*'), '!~test-.* (present, not matching the regular expression)');
    assert.equal(describeMatcher('~x'), '~x (regular expression)');
    assert.equal(isSampleExact('!guest'), false);
});

test('formatRoute shows the proxy target instead of a response', () => {
    const text = formatRoute({ method: 'GET', path: '/x', status: 200, delay: '50ms', proxy: 'https://api.example.com/v1' });
    assert.ok(text.includes('Proxy: https://api.example.com/v1 · delay 50ms'), text);
    assert.ok(!text.includes('Response: 200'), text);
});

test('formatRoute names the fixture file', () => {
    assert.ok(formatRoute({ method: 'GET', path: '/x', status: 200, fixture: 'data/users.json' }).includes('Fixture: data/users.json'));
});

test('formatRoute lists XML requirements', () => {
    const text = formatRoute({ method: 'POST', path: '/soap', matchXml: { 'Envelope.Body.GetQuote.symbol': '*', 'order["@id"]': '!draft' } });
    assert.ok(text.includes('xml Envelope.Body.GetQuote.symbol: '), text);
    assert.ok(text.includes('xml order["@id"]: '), text);
});

test('route curl sends required cookies', () => {
    const route = { method: 'GET', path: '/me', matchCookies: { session: ['*'], theme: ['dark'], debug: ['!'] } };
    assert.equal(buildRouteCurl(route, 'http://localhost:8080'), "curl --cookie 'session=example; theme=dark' 'http://localhost:8080/me'");
    assert.match(formatRoute(route), /Cookie requirements\nsession: \*\ntheme: dark\ndebug: ! \(must be absent\)/);
});

test('JSON paths with quoted keys', () => {
    assert.deepEqual(jsonPathSegments('meta["user.id"].x'), [
        { name: 'meta', any: false, quoted: false },
        { name: 'user.id', any: false, quoted: true },
        { name: 'x', any: false, quoted: false },
    ]);
    assert.equal(jsonPathSegments('a..b'), null);
    assert.equal(sampleJSONBody({ 'labels["app.name"]': 'web', 'items.*.sku': 'A1', 'm["0"]': 'z' }),
        '{"labels":{"app.name":"web"},"items":[{"sku":"A1"}],"m":{"0":"z"}}');
});

test('method lists and any-method routes', () => {
    assert.equal(sampleMethod('GET'), 'GET');
    assert.equal(sampleMethod('OPTIONS,GET'), 'OPTIONS');
    assert.equal(sampleMethod('*'), 'GET');
    assert.equal(methodClass('POST,PUT'), 'method-post');
    assert.equal(methodClass('*'), 'method-any');
    const origin = 'http://localhost:8080';
    assert.equal(buildRouteCurl({ method: 'HEAD,GET', path: '/health' }, origin), "curl --head 'http://localhost:8080/health'");
    assert.equal(buildRouteCurl({ method: 'PUT,PATCH', path: '/x' }, origin), "curl --request PUT 'http://localhost:8080/x'");
    assert.equal(buildRouteCurl({ method: '*', path: '/x' }, origin), "curl 'http://localhost:8080/x'");
});

test('formatRoute shows @fallback and @match.chance', () => {
    const text = formatRoute({ method: '*', path: '/*', fallback: true, chance: '20%' });
    assert.ok(text.includes('Fallback: tried after every other route'));
    assert.ok(text.includes('Chance: answers 20% of matching requests'));
    assert.ok(!formatRoute({ method: 'GET', path: '/x' }).includes('Fallback'));
});

test('formatRoute shows a default Content-Type', () => {
    const text = formatRoute({ method: 'GET', path: '/x', headers: { 'X-Id': ['1'] }, defaultContentType: 'application/json' });
    assert.ok(text.includes('Response headers\nX-Id: 1\nContent-Type: application/json (default, unless guessed from the file extension or body)'));
    assert.ok(formatRoute({ method: 'GET', path: '/x' }).includes('Response headers\n(automatic)'));
});

test('formatRoute shows status templates, @require, @missing and @etag', () => {
    const text = formatRoute({ method: 'GET', path: '/x', status: 200, statusTemplate: '{{$query.status | default 200}}', require: '$fixture.users[id=$path.id]', missing: 'next', etag: true });
    assert.ok(text.includes('Response: {{$query.status | default 200}} · delay 0s'));
    assert.ok(text.includes('Requires: $fixture.users[id=$path.id] (else later routes answer)'));
    assert.ok(text.includes('Missing body file: later routes answer'));
    assert.ok(text.includes('ETag: automatic'));
    assert.ok(formatRoute({ method: 'GET', path: '/x', missing: '410' }).includes('Missing body file: answers 410'));
});

test('formatRoute shows @throttle and a run-time fault override', () => {
    const text = formatRoute({ method: 'GET', path: '/x', status: 200, delay: 'p50 80ms p99 900ms', throttle: '32kb/s', override: 'fault reset 50% (all routes)' });
    assert.ok(text.includes('Response: 200 · delay p50 80ms p99 900ms · throttle 32kb/s'));
    assert.ok(text.includes('Fault override: fault reset 50% (all routes)'));
});

test('comparison matchers get satisfying sample values, like sample.go', () => {
    const cases = { '>100': '101', '>=100': '100', '<0.5': '0', '<=50': '50', '>1.5': '2', '#>2': 'xxx', '#3': 'xxx', '#<5': 'x', '#<=0': '', exact: 'exact' };
    for (const [value, want] of Object.entries(cases)) assert.equal(sampleFor(value), want, value);
    assert.equal(sampleFor('~x+'), null);
    assert.equal(comparison('#fff'), null);
    assert.equal(comparison('>abc'), null);
    assert.ok(!isSampleExact('>=1'));
    assert.equal(sampleJSONBody({ amount: '>100', items: '#>1', code: '#fff' }), '{"amount":101,"items":["example","example"],"code":"#fff"}');
    assert.equal(describeMatcher('#>0'), '#>0 (a length of more than 0)');
    assert.equal(describeMatcher('<=50'), '<=50 (a number at most 50)');
    const curl = buildRouteCurl({ method: 'GET', path: '/a', matchQuery: { limit: ['<=50'] }, matchHeaders: { 'X-Count': ['>=2'] } }, 'http://localhost:8080');
    assert.ok(curl.includes("'http://localhost:8080/a?limit=50'") && curl.includes("'X-Count: 2'"), curl);
});
