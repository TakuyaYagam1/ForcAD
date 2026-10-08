import ipaddress
import re


def validate_data(data, model):
    if not isinstance(data, dict):
        raise ValueError('Ожидается JSON объект')
    result = {
        key: value
        for key, value in data.items()
        if key in model.__slots__ and key != 'id'
    }
    for key in ('active', 'highlighted'):
        if key in model.__slots__:
            value = result.get(key, model.defaults.get(key))
            if not isinstance(value, bool):
                raise ValueError(f'{key}: ожидается true или false')
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
            raise ValueError(f'{key}: непустая строка до {limit} символов')
        value = value.strip()
        if not value or len(value) > limit:
            raise ValueError(f'{key}: непустая строка до {limit} символов')
        result[key] = value
    for key, limit in (('logo_path', 255), ('env_path', 1024)):
        if key in model.__slots__:
            value = result.get(key)
            if value is None:
                value = ''
            if not isinstance(value, str) or len(value) > limit:
                raise ValueError(f'{key}: строка до {limit} символов')
            result[key] = value
    if model.table_name == 'Tasks':
        value = result.get('checker_type')
        if not isinstance(value, str) or len(value.strip()) > 32:
            raise ValueError('checker_type: строка до 32 символов')
        result['checker_type'] = value.strip()
    if model.table_name == 'Teams':
        try:
            ipaddress.ip_address(result['ip'])
        except ValueError as exc:
            raise ValueError('Некорректный IPv4 или IPv6 адрес') from exc
        if not re.fullmatch(r'[A-Za-z0-9_-]{1,16}', result['token']):
            raise ValueError('Некорректный токен команды')
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
                raise ValueError(f'{key}: целое число от {minimum} до 2147483647')
    return result
