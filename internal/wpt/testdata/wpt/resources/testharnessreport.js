setup({ output: false });
add_completion_callback(function(tests, harness_status, asserts) {
    globalThis.__wptResults = {
        harnessStatus: harness_status.status,
        harnessMessage: harness_status.message || "",
        tests: tests.map(function(t) {
            return { name: t.name, status: t.status, message: t.message || "" };
        })
    };
});
