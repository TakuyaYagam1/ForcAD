import ipaddress
import re


def validate_data(data, model):
    if not isinstance(data, dict):
        raise ValueError('Expected a JSON object')
    result = {
        key: value
        for key, value in data.items()
        if key in model.__slots__ and key != 'id'
    }
    for key in ('active', 'highlighted'):
        if key in model.__slots__:
            value = result.get(key, model.defaults.get(key))
            if not isinstance(value, bool):
                raise ValueError(f'{key}: expected true or false')
            result[key] = value
    fields = {'name': 255}
    if model.table_name == 'Teams':
        fields.update(ip=45, token=16)
        result.setdefault('logo_path', '')
    else:
        fields.update(checker=1024)
        result.setdefault('env_path', '')
    for key, limit in fields.items():
        value = result.get(key)
        if not isinstance(value, str):
            raise ValueError(
                f'{key}: expected a nonempty string of at most {limit} characters'
            )
        if key != 'token':
            value = value.strip()
        if not value or len(value) > limit:
            raise ValueError(
                f'{key}: expected a nonempty string of at most {limit} characters'
            )
        result[key] = value
    for key, limit in (('logo_path', 255), ('env_path', 1024)):
        if key in model.__slots__:
            value = result.get(key)
            if value is None:
                value = ''
            if not isinstance(value, str) or len(value) > limit:
                raise ValueError(
                    f'{key}: expected a string of at most {limit} characters'
                )
            result[key] = value
    if model.table_name == 'Tasks':
        value = result.get('checker_type')
        if not isinstance(value, str) or len(value.strip()) > 32:
            raise ValueError('checker_type: expected a string of at most 32 characters')
        result['checker_type'] = value.strip()
    if model.table_name == 'Teams':
        try:
            ipaddress.ip_address(result['ip'])
        except ValueError as exc:
            raise ValueError('Invalid IPv4 or IPv6 address') from exc
        if not re.fullmatch(r'[0-9a-f]{16}', result['token']):
            raise ValueError(
                'Team token must contain exactly 16 lowercase hexadecimal characters'
            )
    else:
        for key in (
            'gets',
            'puts',
            'places',
            'checker_timeout',
            'get_period',
            'default_score',
        ):
            value = result.get(key)
            minimum = 0 if key in ('gets', 'puts', 'default_score') else 1
            if type(value) is not int or not minimum <= value <= 2147483647:
                raise ValueError(
                    f'{key}: expected an integer from {minimum} to 2147483647'
                )
    return result
