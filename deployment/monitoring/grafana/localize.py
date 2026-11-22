"""Generate the four provisioned RU dashboards from the English JSON and UTF-8 catalogs."""

import argparse
import copy
import json
import re
from pathlib import Path

ROOT = Path(__file__).resolve().parent
STEMS = ('forcad', 'traffic', 'ddos', 'relay')
DASHBOARD_PATH = re.compile(r'(/d/forcad-(?:monitoring|traffic|ddos|relay))(?=[/?&#\s"\']|$)')
FIELD_REF = re.compile(r'(__data\.fields\[")([^"\]]+)("\])')
SQL_ALIAS = re.compile(r'\bAS\s+"([^"\n]+)"', re.IGNORECASE)
PRESENTATION_LABEL = re.compile(r',\s*"(column|measure|comparison)"\s*,\s*"([^"\\]+)"')
TEMPLATE = re.compile(r'\{\{[^}]*}}|\$\{[^}]*}|\$\w+')


def russian(source: dict, catalog: dict[str, str], *, require_translations: bool = False) -> dict:
    document = copy.deepcopy(source)
    uid = document['uid']
    missing = set()

    def tr(value):
        return catalog.get(value, value)

    def caption(value):
        visible = TEMPLATE.sub('', value)
        if require_translations and value not in catalog and re.search(r'[a-z]{2,}', visible):
            missing.add(value)
        return tr(value)

    def local_url(value):
        value = DASHBOARD_PATH.sub(r'\1-ru', value)
        return FIELD_REF.sub(lambda m: f'{m[1]}{tr(m[2])}{m[3]}', value)

    def presentation(obj):
        if isinstance(obj, list):
            for item in obj:
                presentation(item)
        elif isinstance(obj, dict):
            for key, value in obj.items():
                if isinstance(value, str):
                    if key in ('title', 'description', 'label', 'legendFormat', 'displayName', 'text', 'noValue'):
                        obj[key] = caption(value)
                    elif key == 'content':
                        obj[key] = local_url(caption(value))
                    elif key == 'url':
                        obj[key] = local_url(value)
                    elif key == 'value' and obj.get('id') == 'displayName':
                        obj[key] = tr(value)
                elif key not in ('current', 'query', 'transformations', 'matcher'):
                    presentation(value)

    def panels(items):
        for panel in items:
            yield panel
            yield from panels(panel.get('panels', ()))

    presentation(document)
    document['uid'] = uid + '-ru'
    document['title'] += ' (RU)'  # Dashboard titles must also be unique in a provisioned folder.
    document['tags'] = ['forcad-ru' if tag == 'forcad' else tag for tag in document['tags']]
    for link in document['links']:
        if link.get('url') == '/d/' + uid + '-ru' and link.get('type') == 'link' and link.get('title') == 'Русский':
            link.update(title='English', url='/d/' + uid)
        elif link.get('type') == 'dashboards':
            link['tags'] = ['forcad-ru' if tag == 'forcad' else tag for tag in link.get('tags', [])]

    for panel in panels(document['panels']):
        if panel['type'] != 'table':
            continue
        # Overrides are display-only: SQL aliases, Grafana transformations, joins,
        # raw label values and PromQL stay byte-identical in both languages.
        overrides = panel.setdefault('fieldConfig', {}).setdefault('overrides', [])
        names = {o['matcher']['options'] for o in overrides if o['matcher']['id'] == 'byName'}
        rename = {}
        measures: dict[str, set[str]] = {}
        for step in panel.get('transformations', []):
            opts = step.get('options', {})
            names.update(opts.get('indexByName', {}))
            names.update(opts.get('fields', {}))
            rename.update(opts.get('renameByName', {}))
        for display_name in rename.values():
            caption(display_name)
        names.difference_update(rename)  # Do not override a renamed field with its raw column caption.
        names.update(rename.values())
        for target in panel.get('targets', []):
            for alias in SQL_ALIAS.findall(target.get('rawSql', '')):
                caption(alias)
                names.add(alias)
            for label, value in PRESENTATION_LABEL.findall(target.get('expr', '')):
                caption(value)
                if label == 'column':
                    names.add(value)
                elif value in catalog and tr(value) != value:
                    measures.setdefault(rename.get(label, label), set()).add(value)
        for name in sorted(names | measures.keys()):
            properties = []
            if tr(name) != name:
                properties.append({'id': 'displayName', 'value': tr(name)})
            if name in measures:
                values = {v: {'text': tr(v)} for v in sorted(measures[name])}
                properties.append({'id': 'mappings', 'value': [{'type': 'value', 'options': values}]})
            if properties:
                overrides.append({'matcher': {'id': 'byName', 'options': name}, 'properties': properties})
    if missing:
        raise ValueError(f'{uid}: missing Russian text: ' + '; '.join(sorted(missing)))
    return document


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--check', action='store_true', help='fail if any provisioned translation is stale')
    args = parser.parse_args()
    for stem in STEMS:
        source = json.loads((ROOT / 'dashboards' / f'{stem}.json').read_text(encoding='utf-8'))
        catalog = json.loads((ROOT / 'i18n' / f'{stem}.ru.json').read_text(encoding='utf-8'))
        rendered = json.dumps(russian(source, catalog, require_translations=True), ensure_ascii=False, indent=2) + '\n'
        path = ROOT / 'dashboards' / f'{stem}.ru.json'
        if args.check:
            if not path.is_file() or path.read_text(encoding='utf-8') != rendered:
                parser.exit(1, f'{path.name} is stale; rerun {Path(__file__).name}\n')
        else:
            path.write_text(rendered, encoding='utf-8')


if __name__ == '__main__':
    main()
