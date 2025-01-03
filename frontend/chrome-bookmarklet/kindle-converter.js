javascript:(function() {
    // Configuration
    const API_ENDPOINT = 'http://localhost:8080/add-to-kindle';

    // Create form data
    const formData = new FormData();
    formData.append('type', 'url');
    formData.append('url', window.location.href);
    formData.append('source_url', document.referrer || '');

    // Send request to backend
    fetch(API_ENDPOINT, {
        method: 'POST',
        body: formData
    })
    .then(response => response.json())
    .then(data => {
        if (data.status === 'success') {
            alert('Successfully sent to Kindle!');
        } else {
            alert('Error: ' + data.message);
        }
    })
    .catch(error => {
        alert('Failed to send to Kindle: ' + error.message);
    });
})();
