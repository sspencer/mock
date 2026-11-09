// The tree view of the fixture editor. renderFixtureTree draws a JSON value
// as nested rows (key, type, value, actions) and reports each edit as a new
// root value through onChange. Edits themselves are the pure functions in
// fixture.mjs; this module only builds DOM and maps events to them.

import {
    addChild, convertValue, duplicateAt, pathLabel, removeAt, renameAt, retype, setAt, valueType,
} from './fixture.mjs';

const types = ['string', 'number', 'boolean', 'null', 'object', 'array'];

const pathKey = (path) => JSON.stringify(path);

// Containers nested deeper than this start collapsed.
const openDepth = 2;

// renderFixtureTree replaces container's children with the tree for root.
// toggled is a Set of path keys whose open state differs from the default; it
// is updated in place so it survives re-renders.
export function renderFixtureTree(container, root, { onChange, onError, toggled }) {
    const isOpen = (path) => toggled.has(pathKey(path)) !== (path.length < openDepth);
    const apply = (edit) => {
        try { onChange(edit(root)); } catch (error) { onError(error.message); }
    };

    function button(label, title, action) {
        const element = document.createElement('button');
        element.type = 'button';
        element.className = 'fx-action';
        element.textContent = label;
        element.title = title;
        element.setAttribute('aria-label', title);
        element.addEventListener('click', action);
        return element;
    }

    function valueEditor(value, path) {
        const type = valueType(value);
        const label = `Value of ${pathLabel(path)}`;
        if (type === 'object' || type === 'array') {
            const summary = document.createElement('span');
            summary.className = 'fx-summary';
            const size = type === 'array' ? value.length : Object.keys(value).length;
            summary.textContent = type === 'array' ? `[${size} item${size === 1 ? '' : 's'}]` : `{${size} field${size === 1 ? '' : 's'}}`;
            return summary;
        }
        if (type === 'null') {
            const empty = document.createElement('span');
            empty.className = 'fx-summary';
            empty.textContent = 'null';
            return empty;
        }
        if (type === 'boolean') {
            const select = document.createElement('select');
            select.className = 'fx-value';
            select.setAttribute('aria-label', label);
            for (const option of ['true', 'false']) {
                const element = document.createElement('option');
                element.value = option; element.textContent = option;
                select.append(element);
            }
            select.value = String(value);
            select.addEventListener('change', () => apply(current => setAt(current, path, select.value === 'true')));
            return select;
        }
        const input = document.createElement('input');
        input.className = `fx-value fx-${type}`;
        input.type = 'text';
        input.spellcheck = false;
        input.value = String(value);
        input.setAttribute('aria-label', label);
        if (type === 'number') input.inputMode = 'decimal';
        input.addEventListener('change', () => {
            const converted = convertValue(input.value, type);
            if (converted.error) {
                input.setAttribute('aria-invalid', 'true');
                onError(`${pathLabel(path)}: ${converted.error}`);
                return;
            }
            input.removeAttribute('aria-invalid');
            apply(current => setAt(current, path, converted.value));
        });
        return input;
    }

    function node(value, path, parentType) {
        const type = valueType(value);
        const container = type === 'object' || type === 'array';
        const open = container && isOpen(path);
        const wrapper = document.createElement('div');
        wrapper.className = 'fx-node';
        wrapper.setAttribute('role', 'treeitem');
        if (container) wrapper.setAttribute('aria-expanded', String(open));
        const row = document.createElement('div');
        row.className = 'fx-row';

        if (container) {
            const toggle = button(open ? '▾' : '▸', `${open ? 'Collapse' : 'Expand'} ${pathLabel(path)}`, () => {
                const key = pathKey(path);
                if (toggled.has(key)) toggled.delete(key); else toggled.add(key);
                onChange(root);
            });
            toggle.classList.add('fx-toggle');
            row.append(toggle);
        } else {
            const spacer = document.createElement('span');
            spacer.className = 'fx-toggle-space';
            row.append(spacer);
        }

        const name = path[path.length - 1];
        if (parentType === 'object') {
            const key = document.createElement('input');
            key.className = 'fx-key';
            key.type = 'text';
            key.spellcheck = false;
            key.value = name;
            key.setAttribute('aria-label', `Name of ${pathLabel(path)}`);
            key.addEventListener('change', () => {
                try {
                    onChange(renameAt(root, path, key.value));
                } catch (error) {
                    key.value = name;
                    onError(error.message);
                }
            });
            row.append(key);
        } else {
            const label = document.createElement('span');
            label.className = 'fx-index';
            label.textContent = parentType === 'array' ? String(name) : 'top level';
            row.append(label);
        }

        const select = document.createElement('select');
        select.className = 'fx-type';
        select.setAttribute('aria-label', `Type of ${pathLabel(path)}`);
        for (const option of types) {
            const element = document.createElement('option');
            element.value = option; element.textContent = option;
            select.append(element);
        }
        select.value = type;
        select.addEventListener('change', () => apply(current => setAt(current, path, retype(value, select.value))));
        row.append(select, valueEditor(value, path));

        const actions = document.createElement('span');
        actions.className = 'fx-actions';
        if (container) {
            actions.append(button('+', type === 'array' ? `Add an item to ${pathLabel(path)}` : `Add a field to ${pathLabel(path)}`, () => {
                const key = pathKey(path);
                if (!open) { if (toggled.has(key)) toggled.delete(key); else toggled.add(key); }
                apply(current => addChild(current, path));
            }));
        }
        if (parentType === 'array') actions.append(button('⧉', `Duplicate ${pathLabel(path)}`, () => apply(current => duplicateAt(current, path))));
        if (path.length) actions.append(button('×', `Remove ${pathLabel(path)}`, () => apply(current => removeAt(current, path))));
        row.append(actions);
        wrapper.append(row);

        if (open) {
            const children = document.createElement('div');
            children.className = 'fx-children';
            children.setAttribute('role', 'group');
            const entries = type === 'array' ? value.map((child, index) => [index, child]) : Object.entries(value);
            for (const [key, child] of entries) children.append(node(child, [...path, key], type));
            if (!entries.length) {
                const empty = document.createElement('p');
                empty.className = 'fx-empty';
                empty.textContent = type === 'array' ? 'No items' : 'No fields';
                children.append(empty);
            }
            wrapper.append(children);
        }
        return wrapper;
    }

    container.replaceChildren(node(root, [], null));
}
