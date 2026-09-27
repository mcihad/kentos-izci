// An unmodified @sentry/node application reporting to izci: only the DSN
// points at izci. Customer and tenant travel as tags.
//
//   npm install @sentry/node
//   IZCI_DSN=http://izk_…@localhost:5300/1 node examples/sentry/node_sentry.cjs

const Sentry = require("@sentry/node");

Sentry.init({
  dsn: process.env.IZCI_DSN,
  environment: "production",
  release: "rapor@2.1.0",
  tracesSampleRate: 0, // izci stores errors; transactions are dropped
});
Sentry.setUser({ id: "3310" });
Sentry.setTag("customer", "tokat-belediyesi");
Sentry.setTag("customer_name", "Tokat Belediyesi");

function raporSablonu(ad) {
  throw new RangeError(`Rapor şablonu bulunamadı: ${ad}`);
}

try {
  raporSablonu("aylik-tahsilat");
} catch (err) {
  console.log("event", Sentry.captureException(err));
}
Sentry.flush(5000).then(() => process.exit(0));
