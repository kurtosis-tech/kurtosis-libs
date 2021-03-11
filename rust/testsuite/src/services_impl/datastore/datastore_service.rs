use anyhow::{Context, Result, anyhow};
use reqwest::blocking::Client;
use kurtosis_rust_lib::services::{service, service_context::ServiceContext};
use reqwest::header::CONTENT_TYPE;
use futures::executor::block_on;

const HEALTHCHECK_URL_SLUG: &str = "health";
const HEALTHY_VALUE: &str = "healthy";
const TEXT_CONTENT_TYPE: &str = "text/plain";
const KEY_ENDPOINT: &str = "key";
const NOT_FOUND_ERR_CODE: u16 = 404;

pub struct DatastoreService {
    service_context: ServiceContext,
    port: u32,
    client: Client,
}

impl DatastoreService {
    pub fn new(service_context: ServiceContext, port: u32) -> DatastoreService {
        return DatastoreService{
            service_context,
            port,
            client: Client::new(),
        };
    }

    pub fn get_ip_address(&self) -> &str {
        return self.service_context.get_ip_address();
    }

    pub fn get_port(&self) -> u32 {
        return self.port;
    }

    // TODO Change error type to Anyhow
    pub fn exists(&self, key: &str) -> Result<bool> {
        self.get_url_for_key(key);

        let url = self.get_url_for_key(key);
        let future = reqwest::get(&url);
        let resp = block_on(future)?;
        let resp_status = resp.status();
        if resp_status.is_success() {
            return Ok(true);
        } else if resp_status.as_u16() == NOT_FOUND_ERR_CODE {
            return Ok(false);
        } else {
            return Err(anyhow!(
                "Got an unexpected HTTP status code: {}", 
                resp_status,
            ));
        }
    }

    pub fn get(&self, key: &str) -> Result<String> {
        let url = self.get_url_for_key(key);
        let resp = self.client
            .get(&url)
            .send()
            .context("An error occurred getting the response after the GET request")?;
        let resp_status = resp.status();
        if !resp_status.is_success() {
            return Err(anyhow!(
                "A non-successful error code was returned: {}", 
                resp_status.as_u16()
            ));
        }
        let resp_body = resp.text()
            .context("Could not read response body")?;
        return Ok(resp_body)
    }

    pub fn upsert(&self, key: &str, value: &str) -> Result<()> {
        let url = self.get_url_for_key(key);
        let client = Client::new();
        let resp = client.post(&url)
            .header(CONTENT_TYPE, TEXT_CONTENT_TYPE)
            .body(value.to_owned())
            .send()
            .context("An error occurred getting the response after the POST request")?;
        let resp_status = resp.status();
        if !resp_status.is_success() {
            return Err(anyhow!(
                "Got non-OK status code: {}", 
                resp_status.as_u16(),
            ));
        }
        return Ok(());
    }

    // ==========================================================================================
    //                                Private helper functions
    // ==========================================================================================
    fn get_url_for_key(&self, key: &str) -> String {
        return format!(
            "http://{}:{}/{}/{}",
            self.get_ip_address(),
            self.get_port(),
            KEY_ENDPOINT,
            key
        );
    }
}

impl service::Service for DatastoreService {
    fn is_available(&self) -> bool {
        let url = format!(
            "http://{}:{}/{}",
            self.service_context.get_ip_address(),
            self.port,
            HEALTHCHECK_URL_SLUG,
        );
        let resp_or_err = self.client
            .get(&url)
            .send();
        if resp_or_err.is_err() {
            debug!(
                "An HTTP error occurred when polling the health endpoint: {}",
                resp_or_err.unwrap_err().to_string()
            );
            return false;
        }
        let resp = resp_or_err.unwrap();
        if !resp.status().is_success() {
            debug!("Received non-OK status code: {}", resp.status().as_u16());
            return false;
        }

        let resp_body_or_err = resp.text();
        if resp_body_or_err.is_err() {
            debug!(
                "An error occurred reading the response body: {}",
                resp_body_or_err.unwrap_err().to_string()
            );
            return false;
        }
        let resp_body = resp_body_or_err.unwrap();
        return resp_body == HEALTHY_VALUE;
    }
}

