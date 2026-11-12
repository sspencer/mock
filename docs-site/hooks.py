"""MkDocs hooks that adapt docs/ pages, written to read well on GitHub, for
the documentation site.

- The "[Documentation](README.md) > Page" breadcrumb at the top of each page
  is dropped; the site has its own navigation.
- Links that leave docs/ (../README.md, ../examples/...) point at the file on
  GitHub, since the site holds only the docs.
- In table rows, "\\|" inside `code` becomes "|". GitHub needs the escape to
  keep the cell together; MkDocs already ignores pipes inside code spans and
  would show the backslash.
"""

import re

REPO = "https://github.com/sspencer/mock"
BRANCH = "master"

BREADCRUMB = re.compile(r"\A\[Documentation\]\(README\.md\)[^\n]*\n+")
OUTSIDE_LINK = re.compile(r"\]\(\.\./([^)\s#]*)(#[^)\s]*)?\)")
CODE_SPAN = re.compile(r"`[^`\n]*`")


def _github_url(match):
    path, anchor = match.group(1), match.group(2) or ""
    kind = "tree" if path == "" or path.endswith("/") else "blob"
    return f"]({REPO}/{kind}/{BRANCH}/{path}{anchor})"


def _unescape_table_pipes(markdown):
    lines = markdown.split("\n")
    for i, line in enumerate(lines):
        if line.lstrip().startswith("|") and "\\|" in line:
            lines[i] = CODE_SPAN.sub(lambda m: m.group(0).replace("\\|", "|"), line)
    return "\n".join(lines)


def on_page_markdown(markdown, page, config, files):
    markdown = BREADCRUMB.sub("", markdown, count=1)
    markdown = _unescape_table_pipes(markdown)
    return OUTSIDE_LINK.sub(_github_url, markdown)
