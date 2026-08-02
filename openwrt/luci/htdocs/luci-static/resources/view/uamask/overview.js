'use strict';
'require dom';
'require fs';
'require poll';
'require uci';
'require view';

var ADAPTER_PATH = '/usr/libexec/uamask-luci',
    POLL_INTERVAL = 5,
    SETTINGS_URL = L.url('admin', 'services', 'uamask', 'settings'),
    activePollTask = null,
    activeGeneration = 0;

function adapterCall(action) {
	return fs.exec(ADAPTER_PATH, [ action ]).then(function(result) {
		var output = result && typeof result.stdout === 'string' ? result.stdout.trim() : '';
		var response;

		if (!output)
			throw new Error(_('The UA-Mask status adapter returned no data.'));

		try {
			response = JSON.parse(output);
		}
		catch (error) {
			throw new Error(_('The UA-Mask status adapter returned invalid data.'));
		}

		if (!response || response.ok !== true)
			throw new Error((response && response.error) || _('The UA-Mask status adapter failed.'));

		return response;
	});
}

function isEnabled(value) {
	return value === true || value === 1 || value === '1' || value === 'yes' || value === 'on';
}

function hasValue(value) {
	if (Array.isArray(value))
		return value.some(function(item) { return String(item || '').trim() !== ''; });

	return value != null && String(value).trim() !== '';
}

function readRiskState() {
	var state = {
		available: true,
		offload: false,
		nonHttp: false,
		keywordOffload: false,
		disconnect: false
	};

	try {
		state.offload = isEnabled(uci.get('UAmask', 'main', 'enable_firewall_set'));
		state.nonHttp = state.offload && isEnabled(uci.get('UAmask', 'main', 'Firewall_ua_bypass'));
		state.keywordOffload = state.offload && hasValue(uci.get('UAmask', 'main', 'Firewall_ua_whitelist'));
		state.disconnect = state.offload && isEnabled(uci.get('UAmask', 'main', 'Firewall_drop_on_match'));
	}
	catch (error) {
		state.available = false;
	}

	return state;
}

function metricText(value, available, suffix) {
	if (!available)
		return _('Not available');

	if (value == null || value === '')
		value = '0';

	return String(value) + (suffix || '');
}

function metricRow(label, value) {
	return E('tr', { 'class': 'tr' }, [
		E('th', { 'class': 'th', 'scope': 'row' }, [ label ]),
		E('td', { 'class': 'td' }, [ value ])
	]);
}

function settingsLink(label) {
	return E('a', {
		'class': 'cbi-button cbi-button-action',
		'href': SETTINGS_URL
	}, [ label ]);
}

function errorDetails(lastError) {
	if (!lastError || !lastError.present)
		return null;

	var details = [];
	if (lastError.stage)
		details.push(_('Stage: ') + String(lastError.stage));
	if (lastError.message)
		details.push(_('Message: ') + String(lastError.message));

	return E('div', { 'class': 'alert-message error' }, [
		E('strong', {}, [ _('Configuration or service error') ]),
		E('p', {}, [ details.join(' — ') || _('The service reported an error without details.') ])
	]);
}

function statusDescription(status) {
	var state = status && status.state;
	var description = {
		'disabled': {
			'className': 'info',
			'label': _('Disabled'),
			'text': _('The service is disabled in the saved configuration.')
		},
		'starting': {
			'className': 'warning',
			'label': _('Starting'),
			'text': _('The service is starting; runtime metrics may not be available yet.')
		},
		'running': {
			'className': 'success',
			'label': _('Running'),
			'text': _('UA-Mask is running and reporting runtime metrics.')
		},
		'failed': {
			'className': 'error',
			'label': _('Failed'),
			'text': _('The service failed to start or stopped unexpectedly.')
		},
		'enabled-not-running': {
			'className': 'error',
			'label': _('Enabled but not running'),
			'text': _('The service is enabled but no running process was detected.')
		}
	};

	if (!description[state]) {
		return {
			'className': 'error',
			'label': _('Unknown'),
			'text': _('The service state could not be determined.')
		};
	}

	return description[state];
}

function statusNotice(status) {
	var info = statusDescription(status);
	var anomaly = status && status.anomaly;
	var text = info.text;

	if (status && status.state === 'running' && anomaly === 'configuration-error') {
		info = {
			'className': 'error',
			'label': _('Running with configuration error'),
			'text': _('The last saved candidate was rejected; the previous valid configuration remains active.')
		};
	}
	else if (anomaly === 'enabled-but-not-running') {
		text = _('The service is enabled but not running. Check the saved configuration and service logs.');
	}

	return E('div', { 'class': 'alert-message ' + info.className }, [
		E('strong', {}, [ info.label ]),
		' — ', text
	]);
}

function serviceInfoTable(status) {
	var version = status && status.version;
	var backend = status && status.backend;
	var backendText = backend === 'nft' ? _('nftables') : backend === 'ipt' ? _('iptables') : (backend || _('Unknown'));

	return E('table', { 'class': 'table cbi-section-table' }, [
		E('tbody', {}, [
			metricRow(_('Status'), statusDescription(status).label),
			metricRow(_('Version'), version || _('Unknown')),
			metricRow(_('Firewall backend'), backendText)
		])
	]);
}

function metricsSection(status) {
	var stats = status && status.stats || {};
	var available = stats.available === true;
	var section = [ E('h3', {}, [ _('Runtime metrics') ]) ];

	if (!available) {
		section.push(E('div', { 'class': 'alert-message info' }, [
			_('Statistics are not available yet. They will appear after the service writes its first five-second snapshot.')
		]));
	}

	section.push(E('table', { 'class': 'table cbi-section-table' }, [
		E('tbody', {}, [
			metricRow(_('Current connections'), metricText(stats.current_connections, available)),
			metricRow(_('Total requests'), metricText(stats.total_requests, available)),
			metricRow(_('Successful modifications'), metricText(stats.successful_modifications, available)),
			metricRow(_('Direct pass-through'), metricText(stats.direct_passthrough, available)),
			metricRow(_('Processing rate'), metricText(stats.rps, available, ' RPS')),
			metricRow(_('Rule processing'), metricText(stats.rule_processing, available)),
			metricRow(_('Cache hits (modified)'), metricText(stats.cache_hit_modify, available)),
			metricRow(_('Cache hits (pass-through)'), metricText(stats.cache_hit_pass, available)),
			metricRow(_('Total cache ratio'), metricText(stats.total_cache_ratio, available, '%'))
		])
	]));

	return E('div', { 'class': 'cbi-section' }, section);
}

function healthSection(status, requestError) {
	if (requestError && !status) {
		return E('div', { 'class': 'cbi-section' }, [
			E('h3', {}, [ _('Configuration health') ]),
			E('div', { 'class': 'alert-message error' }, [
				E('strong', {}, [ _('Unable to check configuration health') ]),
				' — ', _('Retry the status check or open settings to review the saved values.'),
				E('p', {}, [ settingsLink(_('Open settings')) ])
			])
		]);
	}

	var lastError = status && status.last_error;
	var lastErrorNotice = errorDetails(lastError);
	var state = status && status.state;
	var body;

	if (lastErrorNotice) {
		body = [ lastErrorNotice ];
	}
	else if (state === 'failed' || state === 'enabled-not-running') {
		body = [
			E('div', { 'class': 'alert-message warning' }, [
				E('strong', {}, [ _('Attention needed') ]),
				' — ', _('Review the configuration and service state before relying on UA-Mask.'),
				E('p', {}, [ settingsLink(_('Open settings')) ])
			])
		];
	}
	else {
		body = [ E('div', { 'class': 'alert-message success' }, [ _('No configuration or service error is recorded.') ]) ];
	}

	var section = [ E('h3', {}, [ _('Configuration health') ]) ];
	Array.prototype.push.apply(section, body);

	return E('div', { 'class': 'cbi-section' }, section);
}

function riskSection(risk) {
	var notices = [];

	if (!risk.available) {
		notices.push(E('div', { 'class': 'alert-message error' }, [
			E('strong', {}, [ _('Risk settings unavailable') ]),
			' — ', _('The saved offload settings could not be read. Review settings before enabling traffic bypass.')
		]));
	}
	else {
		if (risk.offload) {
			notices.push(E('div', { 'class': 'alert-message warning' }, [
				E('strong', {}, [ _('Traffic offload is enabled') ]),
				' — ', _('Matching destinations bypass UA-Mask and may expose the original User-Agent. Review this leakage risk before relying on the faster firewall path.')
			]));
		}

		if (risk.nonHttp) {
			notices.push(E('div', { 'class': 'alert-message warning' }, [
				E('strong', {}, [ _('Non-HTTP bypass is enabled') ]),
				' — ', _('Non-HTTP traffic can bypass UA-Mask. Encrypted traffic and traffic without a detected cleartext HTTP User-Agent cannot be rewritten.')
			]));
		}

		if (risk.keywordOffload) {
			notices.push(E('div', { 'class': 'alert-message warning' }, [
				E('strong', {}, [ _('User-Agent keyword target offload is configured') ]),
				' — ', _('Substring matches can leave the proxy path, so the original User-Agent may be visible to the destination.')
			]));
		}

		if (risk.disconnect) {
			notices.push(E('div', { 'class': 'alert-message warning' }, [
				E('strong', {}, [ _('Disconnect on User-Agent match is enabled') ]),
				' — ', _('Matching connections are dropped so clients reconnect outside UA-Mask; active sessions may be interrupted.')
			]));
		}

		if (!notices.length) {
			notices.push(E('div', { 'class': 'alert-message info' }, [
				_('Traffic offload and disconnect-on-match are disabled in the saved configuration.')
			]));
		}
	}

	notices.push(E('p', {}, [ settingsLink(_('Review traffic settings')) ]));

	var section = [ E('h3', {}, [ _('Traffic offload and connection risk') ]) ];
	Array.prototype.push.apply(section, notices);

	return E('div', { 'class': 'cbi-section' }, section);
}

return view.extend({
	load: function() {
		return Promise.all([
			L.resolveDefault(uci.load('UAmask'), false),
			adapterCall('status').then(function(status) {
				return { status: status, error: null };
			}, function(error) {
				return { status: null, error: error };
			})
		]);
	},

	render: function(data) {
		var initial = data[1] || {};
		var uciAvailable = data[0] !== false;
		var risk = readRiskState();
		if (!uciAvailable)
			risk.available = false;

		var model = {
			status: initial.status || null,
			requestError: initial.error || null,
			risk: risk
		};
		var generation = ++activeGeneration;
		var mounted = false;
		var inFlight = null;
		var root = E('div', { 'class': 'cbi-map', 'id': 'uamask-overview' });
		var content = E('div');

		if (activePollTask)
			poll.remove(activePollTask);

		root.appendChild(content);

		function isCurrent() {
			return generation === activeGeneration && mounted && document.body && document.body.contains(root);
		}

		function updateView() {
			var nodes = [
				E('h2', {}, [ _('UA-Mask overview') ]),
				statusNotice(model.status)
			];

			if (model.requestError) {
				nodes.push(E('div', { 'class': 'alert-message warning' }, [
					E('strong', {}, [ _('Status refresh failed') ]),
					' — ', _('The last known view is preserved. Retry when the adapter or service is available.'),
					E('p', {}, [ E('button', {
						'class': 'cbi-button cbi-button-action',
						'type': 'button',
						'click': function() { refreshStatus(); }
					}, [ _('Retry now') ]) ])
				]));
			}

			var serviceDetails = [
					E('h3', {}, [ _('Service details') ]),
				serviceInfoTable(model.status)
			];

			if ((model.status && model.status.state !== 'running') || !model.status)
				serviceDetails.push(E('p', {}, [ settingsLink(_('Open settings')) ]));

			nodes.push(E('div', { 'class': 'cbi-section' }, serviceDetails));
			nodes.push(metricsSection(model.status));
			nodes.push(healthSection(model.status, model.requestError));
			nodes.push(riskSection(model.risk));

			dom.content(content, nodes);
		}

		function currentRiskState() {
			var state = readRiskState();
			if (!uciAvailable)
				state.available = false;
			return state;
		}

		function refreshStatus() {
			if (inFlight)
				return inFlight;

			var request = adapterCall('status').then(function(status) {
				if (isCurrent()) {
					model.status = status;
					model.requestError = null;
					model.risk = currentRiskState();
					updateView();
				}
			}, function(error) {
				if (isCurrent()) {
					model.requestError = error;
					model.risk = currentRiskState();
					updateView();
				}
			});

			inFlight = request.then(function(result) {
				inFlight = null;
				return result;
			}, function(error) {
				inFlight = null;
				return error;
			});

			return inFlight;
		}

		updateView();

		var pollTask = function() {
			if (!mounted)
				return;

			if (!document.body || !document.body.contains(root)) {
				poll.remove(pollTask);
				if (activePollTask === pollTask)
					activePollTask = null;
				return;
			}

			return refreshStatus();
		};

		activePollTask = pollTask;
		poll.add(pollTask, POLL_INTERVAL);
		setTimeout(function() { mounted = true; }, 0);

		return root;
	},

	handleSave: null,
	handleSaveApply: null,
	handleReset: null
});
