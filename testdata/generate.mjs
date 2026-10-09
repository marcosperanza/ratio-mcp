import { mkdir, writeFile } from "node:fs/promises";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

const root = dirname(fileURLToPath(import.meta.url));
const fixtureDir = join(root, "fixtures");
const vectorDir = join(root, "vectors");
await mkdir(fixtureDir, { recursive: true });
await mkdir(vectorDir, { recursive: true });

const keyBytes = Uint8Array.from({ length: 32 }, (_, index) => index);
const key = await crypto.subtle.importKey("raw", keyBytes, "AES-GCM", false, ["encrypt"]);
const base64 = (bytes) => Buffer.from(bytes).toString("base64");

async function encryptJSON(value, nonceSeed) {
  const iv = Uint8Array.from({ length: 12 }, (_, index) => (nonceSeed * 17 + index) % 256);
  const plaintext = new TextEncoder().encode(JSON.stringify(value));
  const ciphertext = await crypto.subtle.encrypt({ name: "AES-GCM", iv }, key, plaintext);
  return { encryptedBlob: base64(new Uint8Array(ciphertext)), encryptionIv: base64(iv) };
}

const transactions = [
  {
    id: "fixture-tx-001",
    bookingDate: "2026-09-01",
    blob: { description: "Stipendio settembre", merchantName: "Datore Demo", category: "Salary", amount: 3200, currency: "EUR" },
  },
  {
    id: "fixture-tx-002",
    bookingDate: "2026-09-03",
    blob: { description: "Canone abitazione", merchantName: "Locatore Demo", category: "Rent/Mortgage", amount: -950, currency: "EUR" },
  },
  {
    id: "fixture-tx-003",
    bookingDate: "2026-09-08",
    blob: { description: "Spesa supermercato", merchantName: "Mercato Verde", category: "Groceries", amount: -85.4, currency: "EUR" },
  },
  {
    id: "fixture-tx-004",
    bookingDate: "2026-09-15",
    blob: { description: "Bolletta energia elettrica", merchantName: "Energia Demo", category: "Utilities", amount: -124.55, currency: "EUR" },
  },
  {
    id: "fixture-tx-005",
    bookingDate: "2026-09-20",
    blob: { description: "Saldo carta principale", merchantName: "Banca Demo", category: "Credit Card Payment", amount: -600, currency: "EUR" },
  },
  {
    id: "fixture-tx-006",
    bookingDate: "2026-09-22",
    blob: { description: "Caffè", merchantName: "Bar Centrale", category: "Dining Out", amount: -3.2, currency: "EUR" },
  },
  {
    id: "fixture-tx-007",
    bookingDate: "2026-08-12",
    blob: { description: "Abbonamento video", merchantName: "Streaming Demo", category: "Subscriptions", amount: -12.99, currency: "EUR" },
  },
  {
    id: "fixture-tx-008",
    bookingDate: "2026-07-01",
    blob: { description: "Stipendio luglio", merchantName: "Datore Demo", category: "Salary", amount: 3100, currency: "EUR" },
  },
];

const encryptedTransactions = [];
for (const [index, transaction] of transactions.entries()) {
  const encrypted = await encryptJSON(transaction.blob, index + 1);
  encryptedTransactions.push({
    id: transaction.id,
    workspaceId: "fixture-family",
    bookingDate: transaction.bookingDate,
    ...encrypted,
    serverVersion: index + 1,
  });
}

await writeFile(
  join(fixtureDir, "repository.json"),
  `${JSON.stringify({
    workspaceId: "fixture-family",
    coverage: {
      coverageFrom: "2026-07-01",
      coverageTo: "2026-09-30",
      isComplete: true,
      lastSyncedAt: "2026-09-28T08:00:00Z",
      sourceRevision: "fixture-r1",
    },
    transactions: encryptedTransactions,
  }, null, 2)}\n`,
);

await writeFile(
  join(fixtureDir, "master_key.json"),
  `${JSON.stringify({ workspaceId: "fixture-family", masterKey: base64(keyBytes) }, null, 2)}\n`,
);

const vectorPlaintext = JSON.stringify({
  description: "Browser vector",
  amount: -12.34,
  currency: "EUR",
});
const vectorIV = Uint8Array.from({ length: 12 }, (_, index) => 240 + index);
const vectorCiphertext = await crypto.subtle.encrypt(
  { name: "AES-GCM", iv: vectorIV },
  key,
  new TextEncoder().encode(vectorPlaintext),
);
await writeFile(
  join(vectorDir, "aes_gcm_browser_compatible.json"),
  `${JSON.stringify({
    source: "Web Crypto API crypto.subtle.encrypt",
    algorithm: "AES-GCM",
    masterKey: base64(keyBytes),
    iv: base64(vectorIV),
    ciphertext: base64(new Uint8Array(vectorCiphertext)),
    plaintext: vectorPlaintext,
  }, null, 2)}\n`,
);
