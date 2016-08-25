from __future__ import absolute_import
from __future__ import unicode_literals

import os

import mock
import pytest

from compose.config.environment import Environment
from compose.config.errors import ConfigurationError
from compose.config.interpolation import interpolate_environment_variables
from compose.const import IS_WINDOWS_PLATFORM


@pytest.yield_fixture
def mock_env():
    with mock.patch.dict(os.environ):
        os.environ['USER'] = 'jenny'
        os.environ['FOO'] = 'bar'
        yield


def test_interpolate_environment_variables_in_services(mock_env):
    services = {
        'servicea': {
            'image': 'example:${USER}',
            'volumes': ['$FOO:/target'],
            'logging': {
                'driver': '${FOO}',
                'options': {
                    'user': '$USER',
                }
            }
        }
    }
    expected = {
        'servicea': {
            'image': 'example:jenny',
            'volumes': ['bar:/target'],
            'logging': {
                'driver': 'bar',
                'options': {
                    'user': 'jenny',
                }
            }
        }
    }
    assert interpolate_environment_variables(
        services, 'service', Environment.from_env_file(None)
    ) == expected


def test_interpolate_environment_variables_in_volumes(mock_env):
    volumes = {
        'data': {
            'driver': '$FOO',
            'driver_opts': {
                'max': 2,
                'user': '${USER}'
            }
        },
        'other': None,
    }
    expected = {
        'data': {
            'driver': 'bar',
            'driver_opts': {
                'max': 2,
                'user': 'jenny'
            }
        },
        'other': {},
    }
    assert interpolate_environment_variables(
        volumes, 'volume', Environment.from_env_file(None)
    ) == expected


@pytest.mark.xfail(IS_WINDOWS_PLATFORM, reason="posix only")
def test_interpolate_command(mock_env):
    services = {
        'servicea': {
            'image': 'example:$((echo "FOO"))',
            'volumes': ['$((echo "BAR")):/target'],
            'logging': {
                'driver': '$((echo "BAZ"))',
                'options': {
                    'user': '$((echo "QUX"))',
                }
            }
        }
    }
    expected = {
        'servicea': {
            'image': 'example:FOO',
            'volumes': ['BAR:/target'],
            'logging': {
                'driver': 'BAZ',
                'options': {
                    'user': 'QUX',
                }
            }
        }
    }

    assert interpolate_environment_variables(
        services, 'service', Environment.from_env_file(None)
    ) == expected


@pytest.mark.xfail(IS_WINDOWS_PLATFORM, reason="posix only")
def test_interpolate_bad_command(mock_env):
    services = {
        'servicea': {
            'image': 'example:$((echo "FOO"))',
            'volumes': ['$((echo "BAR")):/target'],
            'logging': {
                'driver': '$((echo "BAZ"))',
                'options': {
                    'user': '$((this is a bad command))',
                }
            }
        }
    }

    try:
        interpolate_environment_variables(
            services, 'service', Environment.from_env_file(None)
        )
    except ConfigurationError:
        pass
    except Exception as e:
        raise e
