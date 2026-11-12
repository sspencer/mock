// Coloring for the inspector's request and response text.

// Bodies longer than this are shown uncolored, to keep the page light.
export const maxHighlightBody = 200_000;

const jsonToken = /"(?:[^"\\\n]|\\.)*"(?=(\s*:)?)|-?\d+(?:\.\d+)?(?:[eE][+-]?\d+)?|\b(?:true|false|null)\b/g;
const verbs = new Set(['get', 'post', 'put', 'patch', 'delete']);

// highlightMessage splits the text formatMessage builds (a start line,
// headers, a blank line, then the body) into {text, kind} tokens. Their text
// concatenates back to the input exactly; kind is '' for plain text. Text
// that does not start like an HTTP message comes back as one plain token.
export function highlightMessage(text) {
    const tokens = [];
    const push = (part, kind = '') => { if (part) tokens.push({ text: part, kind }); };
    const split = text.indexOf('\n\n');
    const lines = (split < 0 ? text : text.slice(0, split)).split('\n');
    let m;
    if ((m = /^(HTTP\/[\d.]+ )(\d)(\d\d.*)$/.exec(lines[0]))) {
        push(m[1], 'version');
        push(m[2] + m[3], `status status-${m[2]}xx`);
    } else if ((m = /^([A-Za-z]+)( \S+)( HTTP\/[\d.]+)$/.exec(lines[0]))) {
        const verb = m[1].toLowerCase();
        push(m[1], `verb verb-${verbs.has(verb) ? verb : 'other'}`);
        push(m[2], 'target');
        push(m[3], 'version');
    } else {
        return text ? [{ text, kind: '' }] : [];
    }
    for (const line of lines.slice(1)) {
        push('\n');
        const colon = line.indexOf(':');
        if (colon > 0) {
            push(line.slice(0, colon), 'name');
            push(line.slice(colon));
        } else {
            push(line);
        }
    }
    if (split >= 0) {
        push('\n\n');
        const body = text.slice(split + 2);
        if (/^\s*[{[]/.test(body) && body.length <= maxHighlightBody) tokens.push(...highlightJSON(body));
        else push(body);
    }
    return tokens;
}

// highlightJSON marks the keys, strings, numbers and literals of JSON text.
// It scans rather than parses, so text that is almost JSON still gets color.
export function highlightJSON(text) {
    const tokens = [];
    let last = 0;
    for (const m of text.matchAll(jsonToken)) {
        if (m.index > last) tokens.push({ text: text.slice(last, m.index), kind: '' });
        const kind = m[0][0] === '"' ? (m[1] !== undefined ? 'name' : 'string') : /^[tfn]/.test(m[0]) ? 'literal' : 'number';
        tokens.push({ text: m[0], kind });
        last = m.index + m[0].length;
    }
    if (last < text.length) tokens.push({ text: text.slice(last), kind: '' });
    return tokens;
}

// tokenClass returns the CSS classes for a token kind.
export function tokenClass(kind) {
    return kind.split(' ').filter(Boolean).map(part => `hl-${part}`).join(' ');
}
