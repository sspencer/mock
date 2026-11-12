// The route inspector shows a route as a sheet: a sign with its method and
// path, a table of facts, then its requirements and response. The sheet is
// built from formatRoute's text, so the two cannot drift apart.

import { methodClass } from './inspector.mjs';

const BODY_LABEL = /^(Body file: |Inline response body$)/;

// parseRouteSheet splits formatRoute's text into
// { method, target, facts: [{label, value}], sections: [{heading, lines}],
//   bodyLabel, body }. Blocks are separated by blank lines; everything after
// the body label is the body, which may hold blank lines itself.
export function parseRouteSheet(text) {
    const blocks = String(text || '').split('\n\n');
    const [first = ''] = blocks;
    const space = first.indexOf(' ');
    const sheet = {
        method: space < 0 ? first : first.slice(0, space),
        target: space < 0 ? '' : first.slice(space + 1),
        facts: [], sections: [], bodyLabel: '', body: '',
    };
    for (let i = 1; i < blocks.length; i++) {
        const block = blocks[i];
        if (BODY_LABEL.test(block)) {
            sheet.bodyLabel = block;
            sheet.body = blocks.slice(i + 1).join('\n\n');
            break;
        }
        const lines = block.split('\n');
        const colon = lines[0].indexOf(': ');
        if (lines.length === 1 && colon > 0) {
            // Parts joined with a middle dot read better as a list.
            sheet.facts.push({ label: lines[0].slice(0, colon), value: lines[0].slice(colon + 2).split(' · ').join(', ') });
        } else {
            sheet.sections.push({ heading: lines[0], lines: lines.slice(1) });
        }
    }
    return sheet;
}

// renderRouteSheet builds the sheet's elements.
export function renderRouteSheet(sheet, doc = document) {
    const make = (tag, className, text) => {
        const node = doc.createElement(tag);
        if (className) node.className = className;
        if (text != null) node.textContent = text;
        return node;
    };
    const sign = make('p', 'sheet-sign');
    sign.append(make('span', methodClass(sheet.method), sheet.method), make('span', 'sheet-target', sheet.target));

    const facts = make('dl', 'sheet-facts');
    for (const { label, value } of sheet.facts) {
        facts.append(make('dt', '', label), make('dd', '', value));
    }

    const nodes = [sign, facts];
    for (const { heading, lines } of sheet.sections) {
        const section = make('section', 'sheet-section');
        section.append(make('h3', '', heading));
        const empty = lines.length === 1 && /^\(.*\)$/.test(lines[0]);
        section.append(make(empty ? 'p' : 'pre', empty ? 'sheet-none' : 'sheet-code', empty ? lines[0].slice(1, -1) : lines.join('\n')));
        nodes.push(section);
    }
    if (sheet.bodyLabel) {
        const section = make('section', 'sheet-section');
        section.append(make('h3', '', sheet.bodyLabel));
        const empty = /^\(.*\)$/.test(sheet.body);
        section.append(make(empty ? 'p' : 'pre', empty ? 'sheet-none' : 'sheet-code sheet-body', empty ? sheet.body.slice(1, -1) : sheet.body));
        nodes.push(section);
    }
    return nodes;
}
