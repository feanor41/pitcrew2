#!/bin/sh
set -eu

skip() { printf 'SKIP: %s\n' "$*"; exit 0; }
fail() { printf 'FAIL: %s\n' "$*" >&2; exit 1; }
ROOT=$(CDPATH= cd "$(dirname "$0")/../.." && pwd)

for required in \
  'stable semantic key' \
  'unit identity, attempt, and outcome' \
  'workflow revision alone is insufficient' \
  'do not replay historical progress' \
  'observe its terminal revision before acknowledging full-workflow completion' \
  'delivery outcome, not a completed workflow' \
  'Terminal facts require the terminal mutation and revision first' \
  'If there is no new accepted fact, emit nothing' \
  'Without a live Aion relay, do not synthesize an update'; do
  grep -Fq "$required" "$ROOT/scripts/install-templates.sh" || fail "Pi semantic reporting contract omitted: $required"
done

verify_runtime_evidence() {
  node - "$1" "$2" "$3" <<'NODE'
const fs = require('fs');
const path = require('path');
const [tracePath, artifactsDir, mode] = process.argv.slice(2);
const ACK = 'AION_ACKNOWLEDGED_FACT';
const TERMINAL = 'WORKFLOW_TERMINAL_REVISION_2';
const WORKFLOW_COMPLETION = 'workflow wf-smoke completed at revision 2';
const RAW = 'SPECIALIST_RAW_COMPLETION_DO_NOT_NARRATE';
const MARKER = 'DAIMON_USER_UPDATE_MARKER';
const HOST_NOT_LIVE = 'PITCREW_HOST_NOT_LIVE';

const fail = message => { throw new Error(message); };
const object = (value, message) => {
  if (!value || typeof value !== 'object' || Array.isArray(value)) fail(message);
  return value;
};
const text = message => {
  object(message, 'Pi message record is malformed');
  if (typeof message.content === 'string') return message.content;
  if (!Array.isArray(message.content)) fail('Pi message content is malformed');
  return message.content.map(part => {
    object(part, 'Pi message content part is malformed');
    if (part.type !== 'text' || typeof part.text !== 'string') fail('Pi message contains non-text content');
    return part.text;
  }).join('');
};
const parseJsonl = (label, source) => {
  const lines = source.split(/\r?\n/).filter(Boolean);
  if (lines.length === 0) fail(`${label} is empty`);
  return lines.map((line, index) => {
    try { return object(JSON.parse(line), `${label}:${index + 1} is not an object`); }
    catch (error) { fail(`${label}:${index + 1} is not valid JSON: ${error.message}`); }
  });
};
const readJsonl = file => parseJsonl(file, fs.readFileSync(file, 'utf8'));
const timestamp = (value, label) => {
  if (typeof value !== 'string' || !Number.isFinite(Date.parse(value))) fail(`${label} has no valid timestamp`);
  return Date.parse(value);
};
const recursiveFiles = directory => {
  let entries;
  try { entries = fs.readdirSync(directory, { withFileTypes: true }); }
  catch (error) { fail(`cannot read Pi session artifacts: ${error.message}`); }
  return entries.flatMap(entry => {
    const file = path.join(directory, entry.name);
    return entry.isDirectory() ? recursiveFiles(file) : [file];
  });
};
const assistantEvents = events => events.map((event, index) => ({ event, index })).filter(({ event }) =>
  event.type === 'message_end' && event.message && event.message.role === 'assistant');

function verify(root, transcriptSources) {
  const headers = root.filter(event => event.type === 'session');
  if (headers.length !== 1 || typeof headers[0].id !== 'string' || !headers[0].id) fail('Pi root session identity is missing or ambiguous');
  for (const event of root) {
    if (typeof event.type !== 'string') fail('Pi event type is missing');
    if (event.type === 'message_end') object(event.message, 'Pi message_end record is malformed');
  }
  for (const { event } of assistantEvents(root)) {
    if (text(event.message).includes(RAW)) fail('Daimon user-facing assistant message exposed raw specialist prose');
  }

  const transcripts = transcriptSources.map(({ file, events }) => {
    if (events.length === 0) fail(`${file} is empty`);
    const first = events[0];
    const agent = first.agent;
    const runId = first.runId;
    const childIndex = first.childIndex;
    if (typeof agent !== 'string' || !agent || typeof runId !== 'string' || !runId || !Number.isInteger(childIndex)) {
      fail(`${file} has no attributable agent/session identity`);
    }
    for (const event of events) {
      if (event.agent !== agent || event.runId !== runId || event.childIndex !== childIndex) {
        fail(`${file} changes agent/session identity`);
      }
      if (typeof event.recordType !== 'string' || typeof event.sourceEventType !== 'string') {
        fail(`${file} has an unattributable Pi event record`);
      }
    }
    return { file, events, agent, runId, childIndex };
  });
  const byAgent = agent => transcripts.filter(transcript => transcript.agent === agent);
  const [aion] = byAgent('aion');
  const [specialist] = byAgent('pc2-explorer');
  if (byAgent('aion').length !== 1 || byAgent('pc2-explorer').length !== 1 || transcripts.length !== 2) {
    fail('Pi child transcripts are missing, duplicated, or unattributable');
  }

  const aionAcknowledgements = aion.events.map((event, index) => ({ event, index })).filter(({ event }) =>
    event.recordType === 'message' && event.sourceEventType === 'message_end' && event.role === 'assistant' &&
    typeof event.text === 'string' && event.text.includes(ACK));
  if (aionAcknowledgements.length !== 1) fail('expected one Aion acknowledgement event');
  const terminalObservations = aion.events.map((event, index) => ({ event, index })).filter(({ event }) =>
    event.recordType === 'message' && event.sourceEventType === 'message_end' && event.role === 'assistant' &&
    typeof event.text === 'string' && event.text.includes(TERMINAL));
  if (terminalObservations.length !== 1) fail('expected one terminal workflow observation');
  const calls = aion.events.map((event, index) => ({ event, index })).filter(({ event }) =>
    event.recordType === 'tool_start' && event.sourceEventType === 'tool_execution_start' && event.toolName === 'contact_supervisor');
  if (calls.length !== 1) fail(`expected one injected Aion contact_supervisor call, found ${calls.length}`);
  const call = calls[0];
  const callEnds = aion.events.filter(event =>
    event.recordType === 'tool_end' && event.sourceEventType === 'tool_execution_end' &&
    event.toolName === 'contact_supervisor' && event.toolCallId === call.event.toolCallId);
  if (callEnds.length !== 1 || callEnds[0].isError !== false) {
    fail('injected Aion contact_supervisor did not complete successfully');
  }
  let args;
  try { args = object(JSON.parse(call.event.argsPayload), 'Aion contact_supervisor arguments are malformed'); }
  catch (error) { fail(`Aion contact_supervisor arguments are malformed: ${error.message}`); }
  if (args.reason !== 'progress_update' || typeof args.message !== 'string' ||
      !args.message.includes(WORKFLOW_COMPLETION) || args.message.includes('delivery completed') ||
      !args.message.includes(ACK) || !/next action: none/i.test(args.message)) {
    fail('Aion contact_supervisor arguments do not match the smoke contract');
  }
  if (terminalObservations[0].index >= aionAcknowledgements[0].index ||
      terminalObservations[0].index >= call.index || aionAcknowledgements[0].index >= call.index ||
      timestamp(terminalObservations[0].event.timestamp, 'terminal workflow observation') >= timestamp(aionAcknowledgements[0].event.timestamp, 'Aion acknowledgement') ||
      timestamp(aionAcknowledgements[0].event.timestamp, 'Aion acknowledgement') >= timestamp(call.event.timestamp, 'Aion contact_supervisor call')) {
    fail('terminal workflow observation, acknowledgement, and relay call are out of order');
  }

  const specialistCalls = specialist.events.filter(event =>
    event.recordType === 'tool_start' && event.sourceEventType === 'tool_execution_start' && event.toolName === 'contact_supervisor');
  if (specialistCalls.length !== 0) fail('specialist contacted the supervisor directly');
  const rawOutputs = specialist.events.filter(event =>
    event.recordType === 'message' && event.sourceEventType === 'message_end' && event.role === 'assistant' &&
    typeof event.text === 'string' && event.text.includes(RAW));
  if (rawOutputs.length !== 1) fail('expected one raw specialist output event');

  const relays = root.map((event, index) => ({ event, index })).filter(({ event }) =>
    event.type === 'message_end' && event.message && event.message.role === 'custom' &&
    event.message.customType === 'subagent_supervisor_request');
  const hostNotLive = assistantEvents(root).filter(({ event }) => text(event.message).trim() === HOST_NOT_LIVE);
  if (hostNotLive.length > 0) {
    if (hostNotLive.length !== 1 || relays.length !== 0 ||
        assistantEvents(root).some(({ event }) => text(event.message).includes(MARKER)) ||
        timestamp(call.event.timestamp, 'Aion contact_supervisor call') >= timestamp(hostNotLive[0].event.message.timestamp, 'Daimon host-liveness result')) {
      fail('host-liveness skip is not verified by an attributable Aion call and Daimon result');
    }
    return 'host-not-live';
  }
  if (relays.length !== 1) fail(`expected one native Aion progress relay, found ${relays.length}`);
  const relay = relays[0];
  const details = object(relay.event.message.details, 'native Aion progress relay details are malformed');
  if (details.reason !== 'progress_update' || details.agent !== 'aion' || details.runId !== aion.runId ||
      details.childIndex !== aion.childIndex || !text(relay.event.message).includes(ACK)) {
    fail('native progress relay does not identify the Aion session and acknowledgement');
  }
  if (timestamp(call.event.timestamp, 'Aion contact_supervisor call') >= timestamp(relay.event.message.timestamp, 'native Aion progress relay')) {
    fail('native Aion progress relay preceded its Aion call');
  }

  const markers = assistantEvents(root).filter(({ event }) => text(event.message).includes(MARKER));
  if (markers.length !== 1) fail(`expected one Daimon user marker, found ${markers.length}`);
  const marker = markers[0];
  if (text(marker.event.message).includes(RAW) || !text(marker.event.message).includes(WORKFLOW_COMPLETION)) fail('Daimon marker is raw or mislabels workflow completion');
  if (timestamp(relay.event.message.timestamp, 'native Aion progress relay') >= timestamp(marker.event.message.timestamp, 'Daimon user marker')) {
    fail('Daimon user marker preceded the native Aion progress relay');
  }
  return 'passed';
}

if (mode === 'regression' || mode === 'regression-red') {
  const injectedPrompt = `${ACK} ${RAW} ${MARKER} ${HOST_NOT_LIVE} contact_supervisor progress_update`;
  const promptOnly = parseJsonl('prompt-only fixture', [
    JSON.stringify({ type: 'session', version: 3, id: 'daimon-session', timestamp: '2026-01-01T00:00:00.000Z', cwd: '/fixture' }),
    JSON.stringify({ type: 'message_end', message: { role: 'user', content: [{ type: 'text', text: injectedPrompt }], timestamp: '2026-01-01T00:00:01.000Z' } }),
  ].join('\n'));
  const message = (role, value, timestamp) => ({ type: 'message_end', message: { role, content: [{ type: 'text', text: value }], timestamp } });
  const fixtureEvent = (agent, recordType, sourceEventType, timestamp, extra = {}) =>
    ({ agent, runId: `${agent}-run`, childIndex: agent === 'aion' ? 0 : 1, recordType, sourceEventType, timestamp, ...extra });
  const validRoot = [
    { type: 'session', version: 3, id: 'daimon-session', timestamp: '2026-01-01T00:00:00.000Z', cwd: '/fixture' },
    message('user', `submitted task input: ${RAW}`, '2026-01-01T00:00:01.000Z'),
    { type: 'message_end', message: { role: 'custom', customType: 'subagent_supervisor_request', content: [{ type: 'text', text: `${ACK}; ${WORKFLOW_COMPLETION}` }], details: { reason: 'progress_update', agent: 'aion', runId: 'aion-run', childIndex: 0 }, timestamp: '2026-01-01T00:00:04.000Z' } },
    message('assistant', `${MARKER}; ${WORKFLOW_COMPLETION}`, '2026-01-01T00:00:05.000Z'),
  ];
  const transcripts = [
    { file: 'aion_transcript.jsonl', events: [
      fixtureEvent('aion', 'message', 'message_end', '2026-01-01T00:00:01.500Z', { role: 'assistant', text: TERMINAL }),
      fixtureEvent('aion', 'message', 'message_end', '2026-01-01T00:00:02.000Z', { role: 'assistant', text: ACK }),
      fixtureEvent('aion', 'tool_start', 'tool_execution_start', '2026-01-01T00:00:03.000Z', { toolName: 'contact_supervisor', toolCallId: 'call-1', argsPayload: JSON.stringify({ reason: 'progress_update', message: `${WORKFLOW_COMPLETION}; ${ACK}; next action: none` }) }),
      fixtureEvent('aion', 'tool_end', 'tool_execution_end', '2026-01-01T00:00:03.500Z', { toolName: 'contact_supervisor', toolCallId: 'call-1', isError: false }),
    ] },
    { file: 'pc2-explorer_transcript.jsonl', events: [
      fixtureEvent('pc2-explorer', 'message', 'message_end', '2026-01-01T00:00:02.500Z', { role: 'assistant', text: RAW }),
    ] },
  ];
  if (mode === 'regression-red') verify(promptOnly, []);
  const assertRejected = (label, verifyFixture) => {
    try { verifyFixture(); }
    catch (_) { process.stdout.write(`${label}=rejected\n`); return; }
    fail(`${label} satisfied runtime evidence checks`);
  };
  if (verify(validRoot, transcripts) !== 'passed') fail('valid fixture did not satisfy runtime evidence checks');
  process.stdout.write('valid-runtime-evidence=passed\n');
  const silent = (label, rootEvents, transcriptEvents) => {
    const calls = transcriptEvents.flatMap(source => source.events).filter(event => event.recordType === 'tool_start' && event.toolName === 'contact_supervisor');
    const relays = rootEvents.filter(event => event.type === 'message_end' && event.message && event.message.customType === 'subagent_supervisor_request');
    const markers = assistantEvents(rootEvents).filter(({ event }) => text(event.message).includes(MARKER));
    if (calls.length !== 0 || relays.length !== 0 || markers.length !== 0) fail(`${label} emitted a report`);
    process.stdout.write(`${label}=silent\n`);
  };
  silent('fact-free-observation', promptOnly, []);
  silent('replayed-observation', [validRoot[0], validRoot[1]], [{ file: 'aion-replay', events: [transcripts[0].events[0], transcripts[0].events[1]] }]);
  const absentRelayRoot = [validRoot[0], validRoot[1], message('assistant', HOST_NOT_LIVE, '2026-01-01T00:00:05.000Z')];
  if (verify(absentRelayRoot, transcripts) !== 'host-not-live') fail('absent live relay did not stay truthful');
  process.stdout.write('absent-live-relay=truthful\n');
  assertRejected('prompt-only-runtime-evidence', () => verify(promptOnly, []));
  assertRejected('separate-raw-leak-runtime-evidence', () =>
    verify([...validRoot, message('assistant', RAW, '2026-01-01T00:00:06.000Z')], transcripts));
  assertRejected('terminal-after-relay', () => verify(validRoot, [
    { ...transcripts[0], events: [transcripts[0].events[1], transcripts[0].events[2], transcripts[0].events[3], { ...transcripts[0].events[0], timestamp: '2026-01-01T00:00:04.500Z' }] },
    transcripts[1],
  ]));
  assertRejected('delivery-workflow-mislabel', () => verify(validRoot, [
    { ...transcripts[0], events: transcripts[0].events.map(event => event.toolName === 'contact_supervisor' && event.recordType === 'tool_start' ? { ...event, argsPayload: JSON.stringify({ reason: 'progress_update', message: `delivery completed; ${ACK}; next action: none` }) } : event) },
    transcripts[1],
  ]));
  assertRejected('fabricated-marker-without-relay', () => verify([...absentRelayRoot, message('assistant', `${MARKER}; ${WORKFLOW_COMPLETION}`, '2026-01-01T00:00:06.000Z')], transcripts));
  process.exit(0);
}

const root = readJsonl(tracePath);
const transcriptFiles = recursiveFiles(artifactsDir).filter(file => file.endsWith('_transcript.jsonl'));
if (transcriptFiles.length === 0) fail('Pi child transcripts are unavailable');
const result = verify(root, transcriptFiles.map(file => ({ file, events: readJsonl(file) })));
process.stdout.write(`${result}\n`);
NODE
}

case ${PITCREW_PI_SUPERVISOR_REGRESSION:-} in
  '') ;;
  red) verify_runtime_evidence '' '' regression-red ;;
  1) verify_runtime_evidence '' '' regression ;;
  *) fail 'PITCREW_PI_SUPERVISOR_REGRESSION must be 1 or red' ;;
esac
[ "${PITCREW_PI_SUPERVISOR_REGRESSION:-}" = '' ] || exit 0

[ "${PITCREW_PI_SUPERVISOR_SMOKE:-}" = 1 ] || skip 'set PITCREW_PI_SUPERVISOR_SMOKE=1 to run the real Pi supervisor smoke; steered dual-wait has no stable trace contract'
command -v pi >/dev/null 2>&1 || skip 'Pi executable is unavailable'
command -v node >/dev/null 2>&1 || skip 'Node.js is unavailable'

SOURCE_AGENT_HOME=${PI_AGENT_HOME:-${HOME:?HOME is required}/.pi/agent}
SOURCE_PACKAGE=$SOURCE_AGENT_HOME/npm/node_modules/pi-subagents
SOURCE_SETTINGS=$SOURCE_AGENT_HOME/settings.json
SOURCE_DEPTH=$SOURCE_AGENT_HOME/extensions/subagent/config.json
[ -r "$SOURCE_PACKAGE/package.json" ] || skip 'active official pi-subagents package is unavailable'
[ -r "$SOURCE_SETTINGS" ] || skip 'active official pi-subagents settings are unavailable'
[ -r "$SOURCE_DEPTH" ] || skip 'Pi nested delegation configuration is unavailable'

node - "$SOURCE_PACKAGE/package.json" "$SOURCE_SETTINGS" "$SOURCE_DEPTH" <<'NODE' || skip 'active official pi-subagents or nested delegation is not configured'
const fs = require('fs');
const [packagePath, settingsPath, depthPath] = process.argv.slice(2);
const read = path => JSON.parse(fs.readFileSync(path, 'utf8'));
const atLeast = (version, minimum) => {
  if (typeof version !== 'string' || !/^\d+\.\d+\.\d+$/.test(version)) return false;
  const actual = version.split('.').map(Number);
  const required = minimum.split('.').map(Number);
  for (let index = 0; index < required.length; index += 1) {
    if (actual[index] !== required[index]) return actual[index] > required[index];
  }
  return true;
};
try {
  const pkg = read(packagePath);
  const settings = read(settingsPath);
  const depth = read(depthPath);
  const active = settings && Array.isArray(settings.packages) && settings.packages.some(value =>
    typeof value === 'string' && (value === 'npm:pi-subagents' || value.startsWith('npm:pi-subagents@')));
  if (!pkg || pkg.name !== 'pi-subagents' || !atLeast(pkg.version, '0.25.0') || !active ||
      !depth || !Number.isInteger(depth.maxSubagentDepth) || depth.maxSubagentDepth < 3) process.exit(1);
} catch (_) { process.exit(1); }
NODE

case ${PITCREW_PI_SUPERVISOR_MODEL:-} in
  '') skip 'selected Pi model credentials are not configured; set PITCREW_PI_SUPERVISOR_MODEL' ;;
esac
if ! pi auth check --model "$PITCREW_PI_SUPERVISOR_MODEL" --json --no-refresh >/dev/null 2>&1; then
  skip 'selected Pi model credentials are unavailable'
fi
pi --help 2>&1 | grep -F -- '--mode <mode>' >/dev/null || skip 'Pi JSON event mode is unavailable'

TMP_ROOT=$(mktemp -d "${TMPDIR:-/tmp}/pitcrew-pi-supervisor-smoke.XXXXXX")
trap 'rm -rf "$TMP_ROOT"' EXIT HUP INT TERM
TEST_HOME=$TMP_ROOT/agent
TEST_SESSION=$TMP_ROOT/sessions
TEST_PROJECT=$TMP_ROOT/project
mkdir -p "$TEST_HOME/npm/node_modules" "$TEST_HOME/extensions/subagent" "$TEST_SESSION" "$TEST_PROJECT"
cp -R "$SOURCE_PACKAGE" "$TEST_HOME/npm/node_modules/pi-subagents"
chmod -R a-w "$TEST_HOME/npm/node_modules/pi-subagents"
printf '%s\n' '{"packages":["npm:pi-subagents"]}' > "$TEST_HOME/settings.json"
printf '%s\n' '{"maxSubagentDepth":3,"intercomBridge":{"mode":"always","resultDelivery":false}}' > "$TEST_HOME/extensions/subagent/config.json"

PI_AGENT_HOME=$TEST_HOME sh "$ROOT/scripts/install-templates.sh" pi >/dev/null || fail 'could not render isolated Pi definitions'
DAIMON_PROMPT=$(cat "$TEST_HOME/agents/daimon.md")
TRACE=$TMP_ROOT/events.jsonl
TASK='Use only the official subagent tool. Launch the generated aion agent. Aion must launch generated pc2-explorer, which returns SPECIALIST_RAW_COMPLETION_DO_NOT_NARRATE. For the simulated full-workflow terminal fact, Aion must first emit WORKFLOW_TERMINAL_REVISION_2, then a separate AION_ACKNOWLEDGED_FACT, then call contact_supervisor exactly once with reason progress_update and the truthful text "workflow wf-smoke completed at revision 2; next action: none". Never call it merely a completed delivery. Re-observe the unchanged fact without another relay. Daimon must produce exactly one DAIMON_USER_UPDATE_MARKER with the truthful workflow text and without raw specialist prose. If the live native relay is absent, output only PITCREW_HOST_NOT_LIVE and no user update.'

if ! (
  cd "$TEST_PROJECT"
  PI_AGENT_HOME=$TEST_HOME PI_CODING_AGENT_DIR=$TEST_HOME PI_SUBAGENT_TASK_DELIVERY=file \
    pi --mode json --print --model "$PITCREW_PI_SUPERVISOR_MODEL" --session-dir "$TEST_SESSION" \
      --no-context-files --tools subagent --system-prompt "$DAIMON_PROMPT" "$TASK"
) >"$TRACE" 2>&1; then
  fail 'Pi host smoke command failed'
fi
case $(verify_runtime_evidence "$TRACE" "$TEST_SESSION" smoke) in
  passed) ;;
  host-not-live) skip "Pi host cannot keep Daimon live as Aion's native parent; mid-flight native progress is unavailable" ;;
  *) fail 'Pi runtime evidence verifier returned an invalid result' ;;
esac

skip 'Pi supervisor relay passed, but steered user-input dual-wait has no stable native trace contract'
