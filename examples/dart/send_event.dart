// Reports a caught error to izci with the Connect protocol (JSON over HTTP),
// using only dart:io. In Flutter use the same request with package:http.
//
//   IZCI_URL=http://localhost:5300 IZCI_KEY=izk_… dart run examples/dart/send_event.dart
//
// Flutter applications can also keep sentry_flutter and point its DSN at
// izci (see docs/sentry.md).

import 'dart:convert';
import 'dart:io';

final url = Platform.environment['IZCI_URL'] ?? 'http://localhost:5300';
final key = Platform.environment['IZCI_KEY'] ??
    (throw StateError('Set IZCI_KEY to an ingest key of the project (izk_…).'));

/// Dart prints frames newest first ("#0  fn (file:line:col)"); izci wants the
/// oldest call first.
List<Map<String, Object>> frames(StackTrace stack) {
  final out = <Map<String, Object>>[];
  final re = RegExp(r'^#\d+\s+(.+?) \((.+?):(\d+)(?::(\d+))?\)$');
  for (final line in stack.toString().split('\n')) {
    final m = re.firstMatch(line.trim());
    if (m == null) continue;
    final file = m.group(2)!;
    out.insert(0, {
      'function': m.group(1)!,
      'filename': file.replaceFirst('file://', ''),
      'lineno': int.parse(m.group(3)!),
      'inApp': !file.startsWith('dart:') && !file.startsWith('package:flutter/'),
    });
  }
  return out;
}

Future<Map<String, dynamic>> report(Object error, StackTrace stack) async {
  final client = HttpClient();
  try {
    final req = await client.postUrl(Uri.parse('$url/izci.ingest.v1.IngestService/SendEvent'));
    req.headers.contentType = ContentType.json;
    req.headers.set(HttpHeaders.authorizationHeader, 'Bearer $key');
    req.write(jsonEncode({
      'event': {
        'level': 'LEVEL_ERROR',
        'platform': 'dart',
        'environment': 'production',
        'release': 'mobil@5.2.0',
        'customer': {'id': 'yozgat-belediyesi', 'name': 'Yozgat Belediyesi'},
        'contexts': {
          'runtime': {'name': 'Dart', 'version': Platform.version.split(' ').first},
          'os': {'name': Platform.operatingSystem, 'version': Platform.operatingSystemVersion},
        },
        'exceptions': [
          {'type': error.runtimeType.toString(), 'value': error.toString(), 'stacktrace': {'frames': frames(stack)}},
        ],
      },
    }));
    final res = await req.close();
    final body = jsonDecode(await res.transform(utf8.decoder).join()) as Map<String, dynamic>;
    if (res.statusCode != 200) throw HttpException('izci answered ${res.statusCode}: ${body['message']}');
    return body['result'] as Map<String, dynamic>;
  } finally {
    client.close();
  }
}

int aboneSayisi(Map<String, Object?> mahalle) => (mahalle['aboneler'] as List).length;

Future<void> main() async {
  try {
    aboneSayisi({'ad': 'Merkez'});
  } catch (error, stack) {
    final result = await report(error, stack);
    print('${result['outcome']} ${result['issueShortId']} ${result['issueUrl']}');
  }
}
