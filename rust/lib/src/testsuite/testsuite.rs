use std::{collections::HashMap};

use crate::networks::network::Network;

use super::{dyn_test::{self, DynTest}, test::Test};

/*
An interface which the user implements to register their tests.
*/
pub trait TestSuite {
	// Get all the tests in the test suite; this is where users will "register" their tests
	fn get_tests(&self) -> HashMap<String, Box<dyn DynTest>>;

	// Determines how many IP addresses will be available in the Docker network created for each test, which determines
	//  the maximum number of services that can be created in the test. The maximum number of services that each
	//  test can have = 2 ^ network_width_bits
	fn get_network_width_bits(&self) -> u32;
}