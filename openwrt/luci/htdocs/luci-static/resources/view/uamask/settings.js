'use strict';
'require view';
'require form';
'require fs';
'require ui';
'require uci';
'require tools.widgets as widgets';

/*
 * UCI list compatibility
 *
 * The init script accepts both the old scalar representation and canonical
 * UCI lists.  The form always presents one value per item and writes an UCI
 * list, while preserving an explicitly empty list with one empty marker.  A
 * missing option is deliberately kept distinct from an empty option so that
 * init fallbacks (notably the keyword defaults) are not changed by a save.
 */
function normalizeItems(value, separator, fallback) {
	if (value === undefined || value === null) {
		value = fallback;
	}

	if (value === undefined || value === null || value === '') {
		return [];
	}

	if (!Array.isArray(value)) {
		value = String(value).split(separator);
	}

	return value.map(function(item) {
		return String(item).trim();
	}).filter(function(item) {
		return item !== '';
	});
}

function attachListCompatibility(option, separator, fallback) {
	var inheritedParse = option.parse;

	option.cfgvalue = function(section_id) {
		var value = uci.get('UAmask', section_id, this.option);

		if (value === undefined || value === null)
			value = this.default !== undefined ? this.default : fallback;

		return normalizeItems(value, separator, fallback);
	};

	option.write = function(section_id, value) {
		var items = normalizeItems(value, separator);

		/*
		 * UCI has no portable zero-length list syntax.  An empty list marker is
		 * understood by the init compatibility reader and is normalized back to
		 * an empty JSON array, rather than activating a missing-value fallback.
		 */
		return uci.set('UAmask', section_id, this.option, items.length ? items : [ '' ]);
	};

	/* DynamicList returns [] when its last item is removed.  AbstractValue's
	 * default parser treats [] as an absent optional value and would delete the
	 * option, which would accidentally re-enable an init fallback on the next
	 * reload.  Write every active, valid array even when its visible values did
	 * not change so an unchanged legacy scalar is canonicalized on save. */
	option.parse = function(section_id) {
		if (this.isActive(section_id) && this.isValid(section_id)) {
			var value = this.formvalue(section_id);
			if (Array.isArray(value))
				return Promise.resolve(this.write(section_id, value));
		}

		return inheritedParse.call(this, section_id);
	};
}

function validateInteger(label, minimum, maximum, allowEmpty) {
	return function(section_id, value) {
		var text = value == null ? '' : String(value).trim();

		if (text === '' && allowEmpty)
			return true;

		if (!/^-?\d+$/.test(text))
			return _('%s must be a whole number.').format(label);

		var number = Number(text);
		if (!isFinite(number) || number < minimum || number > maximum)
			return _('%s must be between %s and %s.').format(label, minimum, maximum);

		return true;
	};
}

function allowBlankDynamicListItem(value) {
	return value == null || String(value).trim() === '';
}

function validatePort(section_id, value) {
	if (allowBlankDynamicListItem(value))
		return true;

	return validateInteger(_('Each bypass port'), 1, 65535, false)(section_id, value);
}

function validateIPv4(section_id, value) {
	if (allowBlankDynamicListItem(value))
		return true;

	var text = value == null ? '' : String(value).trim();

	/* Keep this check explicit even though the LuCI ip4addr datatype also
	 * validates each DynamicList item.  It makes the inline error actionable
	 * on older LuCI releases as well. */
	var parts = text.split('/');
	if (parts.length > 2 || !/^\d{1,3}(?:\.\d{1,3}){3}$/.test(parts[0]))
		return _('Use an IPv4 address, optionally followed by /0 through /32.');

	var octets = parts[0].split('.');
	for (var i = 0; i < octets.length; i++) {
		var octet = Number(octets[i]);
		if (!isFinite(octet) || octet < 0 || octet > 255)
			return _('Each IPv4 octet must be between 0 and 255.');
	}

	if (parts.length === 2 && (!/^\d{1,2}$/.test(parts[1]) || Number(parts[1]) > 32))
		return _('The IPv4 prefix must be between /0 and /32.');

	return true;
}

return view.extend({
	load: function() {
		return uci.load('UAmask');
	},

	render: function() {
		var m, s, o;

		m = new form.Map('UAmask', _('UA-Mask settings'),
			_('Configure how UA-Mask handles detected cleartext HTTP User-Agent headers. Save stores configuration only; Save & Apply validates it with the core and performs one lifecycle reload.'));

		/* Keep the enable flag in a separate section so the status/intent is
		 * visible before users open the detailed tabs. */
		s = m.section(form.NamedSection, 'enabled', 'UAmask', _('Service'));
		s.addremove = false;

		o = s.option(form.Flag, 'enabled', _('Enable UA-Mask'),
			_('When enabled, Save & Apply validates the candidate configuration and starts or reloads the service. Disabling it stops the service through the same transaction.'));
		o.rmempty = false;
		o.default = '0';

		s = m.section(form.NamedSection, 'main', 'UAmask', _('Settings'));
		s.addremove = false;
		s.tab('basic', _('Basics'));
		s.tab('matching', _('User-Agent matching'));
		s.tab('traffic', _('Traffic scope'));
		s.tab('offload', _('Smart offload'));
		s.tab('expert', _('Expert performance'));

		o = s.taboption('basic', form.ListValue, 'operating_profile', _('Performance profile'),
			_('Choose a safe preset first. Low, Medium, and High set the runtime pool and cache automatically; use Custom only when those details are understood.'));
		o.value('Low', _('Low (up to about 200 concurrent workers)'));
		o.value('Medium', _('Medium (up to about 500 concurrent workers)'));
		o.value('High', _('High (up to about 1000 concurrent workers)'));
		o.value('custom', _('Custom'));
		o.default = 'Medium';
		o.rmempty = false;

		o = s.taboption('basic', form.Value, 'ua', _('Replacement User-Agent'),
			_('The User-Agent value used when a detected header matches the selected rule. This does not decrypt HTTPS traffic.'));
		o.default = 'FFF';
		o.rmempty = false;

		o = s.taboption('matching', form.ListValue, 'match_mode', _('Match mode'),
			_('Select which detected cleartext HTTP User-Agent headers are eligible for replacement.'));
		o.value('keywords', _('Keyword substring (recommended)'));
		o.value('regex', _('Regular expression (Go RE2 syntax)'));
		o.value('all', _('All detected cleartext HTTP User-Agent headers'));
		o.default = 'keywords';
		o.rmempty = false;

		o = s.taboption('matching', form.DynamicList, 'keywords', _('Match keyword substrings'),
			_('Add one case-sensitive substring per item. Existing comma-separated scalar values are read as separate items and saved as a canonical UCI list.'));
		o.depends('match_mode', 'keywords');
		o.retain = true;
		o.default = [ 'iPhone', 'iPad', 'Android', 'Macintosh', 'Windows' ];
		o.placeholder = _('Add a keyword');
		o.rmempty = false;
		o.validate = function(section_id, value) {
			if (allowBlankDynamicListItem(value))
				return true;

			return String(value == null ? '' : value).trim() !== '' ? true : _('Add a non-empty keyword or remove the empty item.');
		};
		attachListCompatibility(o, /,/);

		o = s.taboption('matching', form.Value, 'ua_regex', _('User-Agent regular expression'),
			_('Used only in Regular expression mode. The core validates Go RE2 syntax; this page does not claim JavaScript and Go regexes are identical.'));
		o.depends('match_mode', 'regex');
		o.retain = true;
		o.default = '(iPhone|iPad|Android|Macintosh|Windows|Linux|Apple|Mac OS X|Mobile)';
		o.rmempty = false;
		o.validate = function(section_id, value) {
			return String(value == null ? '' : value).trim() !== '' ? true : _('Enter a pattern in Regular expression mode.');
		};

		o = s.taboption('matching', form.ListValue, 'replace_method', _('Regex replacement scope'),
			_('Full replacement substitutes the complete User-Agent. Partial replacement substitutes only text matched by the expression.'));
		o.depends('match_mode', 'regex');
		o.retain = true;
		o.value('full', _('Replace the complete User-Agent'));
		o.value('partial', _('Replace only matching text'));
		o.default = 'full';
		o.rmempty = false;

		o = s.taboption('matching', form.DynamicList, 'whitelist', _('Exact User-Agent pass-through'),
			_('Each item must equal the complete User-Agent string. Matching requests pass through unchanged and remain cacheable; this is not the substring target-offload list below.'));
		o.placeholder = _('Add an exact User-Agent');
		o.optional = true;
		attachListCompatibility(o, /,/);

		o = s.taboption('matching', form.DummyValue, '_all_mode_note', _('All mode scope'),
			_('All mode applies only to User-Agent headers detected in cleartext HTTP. It cannot decrypt HTTPS or rewrite encrypted traffic.'));
		o.depends('match_mode', 'all');

		o = s.taboption('traffic', form.Value, 'port', _('Listening port'),
			_('TCP redirect port. Valid range: 1–65535.'));
		o.datatype = 'port';
		o.default = '12032';
		o.rmempty = false;
		o.validate = validateInteger(_('Listening port'), 1, 65535, false);

		o = s.taboption('traffic', widgets.DeviceSelect, 'iface', _('Ingress devices'),
			_('Select one or more LAN devices whose detected cleartext HTTP traffic should be redirected. Existing space-separated scalar values are read as individual devices.'));
		o.multiple = true;
		o.noaliases = false;
		o.nocreate = false;
		o.optional = false;
		o.default = [ 'br-lan' ];
		o.validate = function(section_id, value) {
			var items = normalizeItems(value, /\s+/);
			return items.length ? true : _('Select at least one ingress device.');
		};
		attachListCompatibility(o, /\s+/, [ 'br-lan' ]);

		o = s.taboption('traffic', form.Flag, 'proxy_host', _('Proxy router-originated traffic'),
			_('Also redirect eligible TCP traffic generated by this router. Disable this when another local proxy must remain outside UA-Mask.'));
		o.default = '0';
		o.rmempty = false;

		o = s.taboption('traffic', form.Value, 'bypass_gid', _('Bypass group ID (GID)'),
			_('Processes in this Linux group are exempt from router-originated redirection. Unit: numeric GID.'));
		o.depends('proxy_host', '1');
		o.retain = true;
		o.datatype = 'uinteger';
		o.default = '65533';
		o.rmempty = false;
		o.validate = validateInteger(_('Bypass GID'), 0, 2147483647, false);

		o = s.taboption('traffic', form.DynamicList, 'bypass_ports', _('Bypass destination ports'),
			_('Add one TCP port per item. Values are exempted before redirection; there is no separator to remember. Valid range: 1–65535.'));
		o.datatype = 'port';
		o.placeholder = _('Port, for example 443');
		o.optional = true;
		o.validate = validatePort;
		attachListCompatibility(o, /\s+/);

		o = s.taboption('traffic', form.DynamicList, 'bypass_ips', _('Bypass destination IPv4/CIDR'),
			_('Add one IPv4 address or CIDR network per item. These destinations are exempted before redirection.'));
		o.datatype = 'ip4addr';
		o.placeholder = _('IPv4 address or CIDR, for example 192.168.0.0/16');
		o.optional = true;
		o.validate = validateIPv4;
		attachListCompatibility(o, /\s+/);

		o = s.taboption('offload', form.Flag, 'enable_firewall_set', _('Enable target offload'),
			_('Risk: a matched target can bypass UA-Mask for up to 24 hours. Its original User-Agent may then be sent directly to that target, so enabling this can leak the original value. The active firewall backend must support the required set package.'));
		o.default = '0';
		o.rmempty = false;

		o = s.taboption('offload', form.Flag, 'Firewall_ua_bypass', _('Bypass detected non-HTTP targets'),
			_('After the decision threshold and delay, a target that remains non-HTTP may be offloaded. This is separate from exact pass-through and from keyword target offload.'));
		o.depends('enable_firewall_set', '1');
		o.retain = true;
		o.default = '0';
		o.rmempty = false;

		o = s.taboption('offload', form.DynamicList, 'Firewall_ua_whitelist', _('Target-offload User-Agent keywords'),
			_('Each item is a case-sensitive substring. A match skips rewriting and schedules the target IP and port for direct offload; the original User-Agent can leak to the target for up to 24 hours. This is not exact pass-through.'));
		o.depends('enable_firewall_set', '1');
		o.retain = true;
		o.placeholder = _('Add a substring keyword');
		o.optional = true;
		o.validate = function(section_id, value) {
			if (allowBlankDynamicListItem(value))
				return true;

			return String(value == null ? '' : value).trim() !== '' ? true : _('Use a non-empty offload keyword or remove the empty item.');
		};
		attachListCompatibility(o, /,/);

		o = s.taboption('offload', form.Flag, 'Firewall_drop_on_match', _('Disconnect current connection on keyword match'),
			_('Risk: the matching HTTP connection is deliberately dropped so the client reconnects outside UA-Mask. This can disrupt applications and, with target offload enabled, exposes the original User-Agent. Enable only when that consequence is intentional.'));
		o.depends({ enable_firewall_set: '1', Firewall_ua_whitelist: /.+/ });
		o.retain = true;
		o.default = '0';
		o.rmempty = false;

		o = s.taboption('expert', form.Flag, 'firewall_advanced_settings', _('Customize offload decision policy'),
			_('Keep the safe defaults unless you need to tune non-HTTP detection. The init script enforces minimum safety floors before generating the core configuration.'));
		o.default = '0';
		o.rmempty = false;

		o = s.taboption('expert', form.Value, 'firewall_nonhttp_threshold', _('Non-HTTP detection threshold'),
			_('Consecutive non-HTTP connections before a target is considered for offload. Unit: connections. Minimum: 1.'));
		o.depends('firewall_advanced_settings', '1');
		o.retain = true;
		o.datatype = 'uinteger';
		o.default = '5';
		o.rmempty = false;
		o.validate = validateInteger(_('Non-HTTP detection threshold'), 1, 2147483647, false);

		o = s.taboption('expert', form.Value, 'firewall_decision_delay', _('Offload decision delay'),
			_('Time to observe a candidate after it reaches the threshold. Unit: seconds. Minimum safety floor: 10 seconds.'));
		o.depends('firewall_advanced_settings', '1');
		o.retain = true;
		o.datatype = 'uinteger';
		o.default = '60';
		o.rmempty = false;
		o.validate = validateInteger(_('Offload decision delay'), 10, 2147483647, false);

		o = s.taboption('expert', form.Value, 'firewall_timeout', _('Target offload timeout'),
			_('How long a target bypass remains in the firewall set. Unit: seconds. Minimum safety floor: 60 seconds; the default is 28800 seconds (8 hours).'));
		o.depends('firewall_advanced_settings', '1');
		o.retain = true;
		o.datatype = 'uinteger';
		o.default = '28800';
		o.rmempty = false;
		o.validate = validateInteger(_('Target offload timeout'), 60, 2147483647, false);

		o = s.taboption('expert', form.Value, 'buffer_size', _('I/O buffer size'),
			_('Per-connection buffer. Unit: bytes. Valid range: 1024–65536. Larger buffers can improve throughput but consume more memory.'));
		o.depends('operating_profile', 'custom');
		o.retain = true;
		o.datatype = 'range(1024,65536)';
		o.default = '8192';
		o.rmempty = false;
		o.validate = validateInteger(_('I/O buffer size'), 1024, 65536, false);

		o = s.taboption('expert', form.Value, 'pool_size', _('Worker pool size'),
			_('Number of workers reserved for connections. Unit: workers. Zero keeps one goroutine per connection; larger values can reduce GC pressure at the cost of memory.'));
		o.depends('operating_profile', 'custom');
		o.retain = true;
		o.datatype = 'uinteger';
		o.default = '0';
		o.rmempty = false;
		o.validate = validateInteger(_('Worker pool size'), 0, 2147483647, false);

		o = s.taboption('expert', form.Value, 'cache_size', _('LRU cache size'),
			_('Number of cached rewrite/pass-through decisions. Unit: entries. Larger caches use more memory.'));
		o.depends('operating_profile', 'custom');
		o.retain = true;
		o.datatype = 'uinteger';
		o.default = '2000';
		o.rmempty = false;
		o.validate = validateInteger(_('LRU cache size'), 0, 2147483647, false);

		o = s.taboption('expert', form.Value, 'gogc_value', _('Go garbage-collection target (GOGC)'),
			_('Expert-only Go runtime tuning. Unit: percent. Values >= 0 use the normal target; -1 disables automatic GC. The safe default is 100.'));
		o.depends('operating_profile', 'custom');
		o.retain = true;
		o.datatype = 'integer';
		o.default = '100';
		o.rmempty = false;
		o.validate = validateInteger(_('GOGC'), -1, 2147483647, false);

		o = s.taboption('expert', form.ListValue, 'log_level', _('Log level'),
			_('Use Debug only while diagnosing a problem; Info is the normal default.'));
		o.value('debug', _('Debug'));
		o.value('info', _('Info'));
		o.value('warn', _('Warning'));
		o.value('error', _('Error'));
		o.value('fatal', _('Fatal'));
		o.value('panic', _('Panic'));
		o.default = 'info';
		o.rmempty = false;

		o = s.taboption('expert', form.Value, 'log_file', _('Application log path'),
			_('Leave blank to disable file logging. Diagnostics reads only canonical UA-Mask log directories and never interpolates this value into a shell command.'));
		o.placeholder = '/tmp/UAmask/UAmask.log';
		o.optional = true;

		return Promise.resolve(m.render());
	},

	/* Save only stages UCI changes. Save & Apply commits them first, then makes
	 * exactly one explicit reload request; reload_service handles enabled,
	 * disabled, validation, and recovery transitions authoritatively. */
	handleSaveApply: function(ev) {
		return this.handleSave(ev)
			.then(function() {
				return uci.apply();
			})
			.then(function() {
				return fs.exec('/etc/init.d/UAmask', [ 'reload' ]);
			})
			.then(function(result) {
				if (!result || result.code !== 0) {
					var detail = result && (result.stderr || result.stdout);
					var message = _('UA-Mask reload failed.');

					if (detail)
						message += ' ' + String(detail).trim();

					throw new Error(message);
				}

				ui.changes.setIndicator(0);
				ui.addNotification(null, E('p', _('Settings saved and the UA-Mask lifecycle action completed.')), 'info');
				return result;
			})
			.catch(function(error) {
				var message = error && error.message ? error.message : _('The configuration could not be applied.');
				var diagnostics = E('a', {
					'href': L.url('admin/services/uamask/diagnostics')
				}, _('Open Diagnostics'));

				ui.addNotification(null, E('p', [ message, ' ', diagnostics ]), 'error');
				return Promise.reject(error);
			});
	}
});
