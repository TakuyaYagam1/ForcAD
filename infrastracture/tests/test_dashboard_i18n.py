"""A locale switch must preserve the evidence query, field links and filters."""

import importlib.util
import unittest
from pathlib import Path

GRAFANA = Path(__file__).resolve().parents[2] / 'deployment/monitoring/grafana'
spec = importlib.util.spec_from_file_location('grafana_localize', GRAFANA / 'localize.py')
assert spec and spec.loader
localize = importlib.util.module_from_spec(spec)
spec.loader.exec_module(localize)


class DashboardLanguages(unittest.TestCase):
    def test_russian_switch_keeps_raw_data_and_field_drilldown(self):
        doc = {
            'uid': 'forcad-relay', 'title': 'Relay', 'tags': ['forcad'],
            'links': [{'title': 'Русский', 'type': 'link', 'url': '/d/forcad-relay-ru', 'includeVars': True, 'keepTime': True}],
            'panels': [{'id': 1, 'title': 'Source', 'type': 'table',
                        'targets': [{'expr': 'source_bytes{source="Source"}', 'rawSql': 'SELECT ip AS "Source"'}],
                        'transformations': [{'id': 'organize', 'options': {'renameByName': {'ip': 'Source'}}}],
                        'fieldConfig': {'overrides': [{'matcher': {'id': 'byName', 'options': 'Source'}, 'properties': [
                            {'id': 'links', 'value': [{'title': 'Investigate', 'url': '/d/forcad-traffic?var-source=${__data.fields["Source"]}'}]},
                        ]}]}}],
        }
        ru = localize.russian(doc, {'Relay': 'Отказы', 'Source': 'Источник', 'Investigate': 'Проверить'})
        self.assertEqual(doc['uid'], 'forcad-relay')
        self.assertEqual(ru['links'][0], {**doc['links'][0], 'title': 'English', 'url': '/d/forcad-relay'})
        panel = ru['panels'][0]
        self.assertEqual(panel['title'], 'Источник')
        self.assertEqual(panel['targets'], doc['panels'][0]['targets'])
        self.assertEqual(panel['transformations'], doc['panels'][0]['transformations'])
        props = [prop for o in panel['fieldConfig']['overrides'] for prop in o['properties']]
        self.assertIn({'id': 'displayName', 'value': 'Источник'}, props)
        link = next(p for p in props if p['id'] == 'links')['value'][0]
        self.assertEqual(link['url'], '/d/forcad-traffic-ru?var-source=${__data.fields["Источник"]}')
        self.assertEqual(link['title'], 'Проверить')
