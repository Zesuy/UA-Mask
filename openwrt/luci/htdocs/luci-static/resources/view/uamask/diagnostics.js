'use strict';
'require view';
'require fs';
'require ui';

/*
 * The adapter is the only diagnostics boundary exposed to the browser.  Its
 * action list is deliberately fixed here as well as in the shell adapter: no
 * configured path, command, or other browser value is ever passed through.
 */
var ADAPTER = '/usr/libexec/uamask-luci';

function failure(action, message) {
	return {
		ok: false,
		action: action,
		error: message || _('The diagnostics action could not be completed.')
	};
}

function errorMessage(response, fallback) {
	if (response && response.error)
		return String(response.error);

	return fallback || _('The diagnostics action could not be completed.');
}

function parseReply(action, result) {
	var output = result && typeof result.stdout == 'string' ? result.stdout.trim() : '';
	var response;

	if (!output)
		return failure(action, _('The adapter returned no response.'));

	try {
		response = JSON.parse(output);
	} catch (e) {
		return failure(action, _('The adapter returned invalid diagnostics data.'));
	}

	if (!response || typeof response != 'object')
		return failure(action, _('The adapter returned an unexpected response.'));

	/* A non-zero adapter exit still carries a structured, actionable error. */
	if (response.ok !== true && !response.error)
		return failure(action, _('The adapter rejected the diagnostics action.'));

	return response;
}

function adapter(action, extra) {
	var params = [ action ];

	if (Array.isArray(extra))
		params = params.concat(extra);

	return fs.exec(ADAPTER, params).then(function(result) {
		return parseReply(action, result);
	}, function(error) {
		return failure(action, error && error.message ? error.message : _('The adapter could not be reached.'));
	});
}

function loadDiagnostics() {
	return Promise.all([
		adapter('status'),
		adapter('logs'),
		adapter('effective-config'),
		adapter('last-error')
	]);
}

function button(label, click, disabled, kind) {
	return E('button', {
		'class': 'btn cbi-button ' + (kind || 'cbi-button-action'),
		'type': 'button',
		/* HTML boolean attributes are disabled by presence, so false must
		 * become null rather than the literal attribute value "false". */
		'disabled': disabled ? '' : null,
		'click': click
	}, label);
}

function settingsLink(label) {
	return E('a', {
		'class': 'btn cbi-button cbi-button-action',
		'href': L.url('admin/services/uamask/settings')
	}, label);
}

function valueRow(label, value) {
	return E('div', { 'class': 'cbi-value' }, [
		E('div', { 'class': 'cbi-value-title' }, label),
		E('div', { 'class': 'cbi-value-field' }, [ value == null || value === '' ? _('—') : String(value) ])
	]);
}

function stateLabel(state) {
	return {
		disabled: _('Disabled'),
		starting: _('Starting'),
		running: _('Running'),
		failed: _('Failed'),
		'enabled-not-running': _('Enabled but not running')
	}[state] || (state ? String(state) : _('Unknown'));
}

function statusSection(response, busy, refresh) {
	if (!response || response.ok !== true) {
		return E('div', { 'class': 'cbi-section' }, [
			E('legend', {}, _('Service state')),
			E('div', { 'class': 'alert-message', 'role': 'alert' }, [
				E('p', {}, [ errorMessage(response, _('Unable to read the service state.')) ]),
				button(_('Retry'), refresh, busy)
			])
		]);
	}

	var state = response.state || response.status || '';
	var anomaly = response.anomaly || '';
	var recovery = null;

	if (anomaly == 'enabled-but-not-running' || state == 'enabled-not-running') {
		recovery = E('div', { 'class': 'alert-message', 'role': 'alert' }, [
			E('p', {}, _('UA-Mask is enabled, but no running process was detected. Check the configuration and retry the service action.')),
			E('div', { 'class': 'right' }, [
				settingsLink(_('Open settings')),
				button(_('Retry'), refresh, busy)
			])
		]);
	} else if (anomaly == 'configuration-error') {
		recovery = E('div', { 'class': 'alert-message', 'role': 'alert' }, [
			E('p', {}, _('The running process is healthy, but the latest candidate configuration was rejected. Review the error below and apply a corrected configuration.')),
			E('div', { 'class': 'right' }, [
				settingsLink(_('Open settings')),
				button(_('Retry'), refresh, busy)
			])
		]);
	} else if (anomaly) {
		recovery = E('div', { 'class': 'alert-message', 'role': 'alert' }, [
			E('p', {}, [ _('The adapter reported an anomalous service state: %s.').format(String(anomaly)) ]),
			E('div', { 'class': 'right' }, [
				settingsLink(_('Open settings')),
				button(_('Retry'), refresh, busy)
			])
		]);
	} else if (state == 'failed') {
		recovery = E('div', { 'class': 'alert-message', 'role': 'alert' }, [
			E('p', {}, _('The service failed to validate or start. Review the latest error and try again after correcting the configuration.')),
			E('div', { 'class': 'right' }, [
				settingsLink(_('Open settings')),
				button(_('Retry'), refresh, busy)
			])
		]);
	} else if (state == 'starting') {
		recovery = E('div', { 'class': 'alert-message notice', 'role': 'status' },
			_('The service is starting. Refresh after a moment to confirm the final state.'));
	}

	var section = [
		E('legend', {}, _('Service state')),
		valueRow(_('Current state'), stateLabel(state)),
		valueRow(_('Enabled'), response.enabled ? _('Yes') : _('No')),
		valueRow(_('Process ID'), response.pid),
		valueRow(_('Installed version'), response.version),
		valueRow(_('Firewall backend'), response.backend)
	];

	if (recovery)
		section.push(recovery);

	section.push(E('div', { 'class': 'right' }, [button(_('Refresh'), refresh, busy)]));

	return E('div', { 'class': 'cbi-section' }, section);
}

function logsSection(response, busy, refresh, clearLogs) {
	if (!response || response.ok !== true) {
		return E('div', { 'class': 'cbi-section' }, [
			E('legend', {}, _('Recent logs')),
			E('div', { 'class': 'alert-message', 'role': 'alert' }, [
				E('p', {}, [ errorMessage(response, _('The configured log is unavailable.')) ]),
				E('p', { 'class': 'cbi-section-descr' }, _('The adapter reads only the fixed UA-Mask log location.')),
				button(_('Retry'), refresh, busy)
			])
		]);
	}

	var content = response.content || '';
	var empty = content === '';

	return E('div', { 'class': 'cbi-section' }, [
		E('legend', {}, _('Recent logs')),
		E('p', { 'class': 'cbi-section-descr' }, [ response.lines != null ?
			_('Showing the most recent %s lines.').format(response.lines) : _('Showing the most recent 200 lines.') ]),
		E('textarea', {
			'class': 'cbi-input-text',
			'readonly': 'readonly',
			'rows': 18,
			'wrap': 'soft',
			'spellcheck': 'false',
			'aria-label': _('Recent UA-Mask logs'),
			'style': 'box-sizing:border-box;width:100%;max-width:100%;min-height:18em;resize:vertical;white-space:pre-wrap;overflow-wrap:anywhere;word-break:break-word'
		}, [ empty ? _('No log lines are available.') : content ]),
		E('div', { 'class': 'right' }, [
			button(_('Refresh logs'), refresh, busy),
			button(_('Clear logs'), clearLogs, busy, 'cbi-button-negative')
		])
	]);
}

function effectiveConfigSection(response, busy, refresh) {
	if (!response || response.ok !== true) {
		return E('div', { 'class': 'cbi-section' }, [
			E('legend', {}, _('Effective configuration')),
			E('div', { 'class': 'alert-message', 'role': 'alert' }, [
				E('p', {}, [ errorMessage(response, _('The effective configuration is unavailable.')) ]),
				button(_('Retry'), refresh, busy)
			])
		]);
	}

	var content = response.content || response.effective_config || '';

	return E('div', { 'class': 'cbi-section' }, [
		E('legend', {}, _('Effective configuration')),
		E('p', { 'class': 'cbi-section-descr' }, _('This is the core-authoritative JSON currently installed for UA-Mask. It is displayed as text and is not editable here.')),
		E('textarea', {
			'class': 'cbi-input-text',
			'readonly': 'readonly',
			'rows': 18,
			'wrap': 'soft',
			'spellcheck': 'false',
			'aria-label': _('Effective UA-Mask JSON configuration'),
			'style': 'box-sizing:border-box;width:100%;max-width:100%;min-height:18em;resize:vertical;white-space:pre-wrap;overflow-wrap:anywhere;word-break:break-word'
		}, [ content || _('No effective configuration is available.') ])
	]);
}

function errorSection(response, statusResponse, busy, refresh) {
	if (!response || response.ok !== true) {
		return E('div', { 'class': 'cbi-section' }, [
			E('legend', {}, _('Latest validation or service error')),
			E('div', { 'class': 'alert-message', 'role': 'alert' }, [
				E('p', {}, [ errorMessage(response, _('The latest error record could not be read.')) ]),
				button(_('Retry'), refresh, busy)
			])
		]);
	}

	var lastError = response.last_error || {};

	if (!lastError.present) {
		var status = statusResponse && statusResponse.ok === true ? (statusResponse.state || statusResponse.status || '') : '';
		var anomaly = statusResponse && statusResponse.ok === true ? statusResponse.anomaly : '';
		var artifactMissing = status == 'failed' || status == 'enabled-not-running' || anomaly == 'enabled-but-not-running';

		return E('div', { 'class': 'cbi-section' }, [
			E('legend', {}, _('Latest validation or service error')),
			E('div', { 'class': artifactMissing ? 'alert-message' : 'alert-message notice', 'role': artifactMissing ? 'alert' : 'status' }, [
				E('p', {}, artifactMissing ?
					_('The service is anomalous, but no error artifact is available. Retry to regenerate diagnostics and check the service state.') :
					_('No validation or service-start error is recorded.')),
				E('p', { 'class': 'cbi-section-descr' }, artifactMissing ?
					_('The expected error artifact may be missing or unreadable.') :
					_('The error artifact is absent or was cleared after a successful recovery.'))
			])
		]);
	}

	return E('div', { 'class': 'cbi-section' }, [
		E('legend', {}, _('Latest validation or service error')),
		E('div', { 'class': 'alert-message', 'role': 'alert' }, [
			E('p', {}, _('A previous validation or service-start attempt failed. This record may be stale; compare it with the current service state above.')),
			valueRow(_('Failure stage'), lastError.stage),
			valueRow(_('Failure message'), lastError.message)
		])
	]);
}

return view.extend({
	load: loadDiagnostics,

	render: function(initial) {
		var state = {
			status: initial && initial[0] ? initial[0] : failure('status'),
			logs: initial && initial[1] ? initial[1] : failure('logs'),
			effective: initial && initial[2] ? initial[2] : failure('effective-config'),
			error: initial && initial[3] ? initial[3] : failure('last-error'),
			busy: false,
			feedback: '',
			errorFeedback: false
		};
		var root = E('div', { 'class': 'cbi-map uamask-diagnostics', 'aria-live': 'polite' });

		function refresh(message) {
			if (state.busy)
				return Promise.resolve();

			state.busy = true;
			state.feedback = message || _('Refreshing diagnostics…');
			state.errorFeedback = false;
			draw();

			return loadDiagnostics().then(function(results) {
				state.status = results[0];
				state.logs = results[1];
				state.effective = results[2];
				state.error = results[3];
				state.busy = false;
				state.errorFeedback = results.some(function(response) { return !response || response.ok !== true; });
				state.feedback = state.errorFeedback ? _('Some diagnostics could not be read. Use Retry after addressing the reported error.') : (message || _('Diagnostics refreshed.'));
				draw();
			});
		}

		function clearLogs() {
			if (state.busy)
				return;

			ui.showModal(_('Clear UA-Mask logs'), [
				E('p', {}, _('Clear the fixed UA-Mask application log? This cannot be undone.')),
				E('div', { 'class': 'right' }, [
					button(_('Cancel'), ui.hideModal, false, 'cbi-button'),
					button(_('Clear logs'), function() {
						ui.hideModal();
						state.busy = true;
						state.feedback = _('Clearing logs…');
						state.errorFeedback = false;
						draw();

						adapter('clear-log', [ 'confirm' ]).then(function(response) {
							if (!response || response.ok !== true) {
								state.busy = false;
								state.feedback = errorMessage(response, _('The log could not be cleared.'));
								state.errorFeedback = true;
								draw();
								return;
							}

							/* Let the next read confirm the clear and refresh every panel. */
							state.busy = false;
							refresh(_('Logs cleared.'));
						});
					}, false, 'cbi-button-negative')
				])
			]);
		}

		function draw() {
			var nodes = [
				E('h2', {}, _('Diagnostics')),
				E('p', { 'class': 'cbi-map-descr' }, _('Review bounded logs, the core-effective configuration, and the latest validation or service-start result. Browser input never selects a file or command.'))
			];

			if (state.feedback) {
				var feedbackNodes = [ E('p', {}, [ state.feedback ]) ];

				if (state.errorFeedback)
					feedbackNodes.push(button(_('Retry'), function() { refresh(); }, state.busy));

				nodes.push(E('div', {
					'class': state.errorFeedback ? 'alert-message' : 'alert-message notice',
					'role': state.errorFeedback ? 'alert' : 'status',
					'aria-live': 'polite'
				}, feedbackNodes));
			}

			nodes.push(statusSection(state.status, state.busy, function() { refresh(); }));
			nodes.push(logsSection(state.logs, state.busy, function() { refresh(); }, clearLogs));
			nodes.push(effectiveConfigSection(state.effective, state.busy, function() { refresh(); }));
			nodes.push(errorSection(state.error, state.status, state.busy, function() { refresh(); }));

			root.replaceChildren.apply(root, nodes);
			root.setAttribute('aria-busy', state.busy ? 'true' : 'false');
		}

		draw();
		return root;
	},

	handleSave: null,
	handleSaveApply: null,
	handleReset: null
});
