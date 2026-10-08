// Static Semgrep fixture only. Never import or execute this file.
function review(source, node, text) {
  // ruleid: forcad-js-dynamic-code
  eval(source);
  // ruleid: forcad-js-dynamic-code
  new Function(source);
  // ok: forcad-js-dynamic-code
  JSON.parse(source);

  // ruleid: forcad-js-document-write
  document.writeln(text);
  // ruleid: forcad-js-inner-html
  node.innerHTML = text;
  // ok: forcad-js-inner-html
  node.textContent = text;

  // ruleid: forcad-js-disabled-tls
  process.env.NODE_TLS_REJECT_UNAUTHORIZED = "0";
  // ruleid: forcad-js-disabled-tls
  const tlsOptions = { rejectUnauthorized: false };
  // ok: forcad-js-disabled-tls
  const safeOptions = { rejectUnauthorized: true };
  return tlsOptions && safeOptions;
}
