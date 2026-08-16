INSERT INTO queries (keywords, location, queried_at, updated_at) VALUES
('python', 'san francisco', CURRENT_TIMESTAMP - INTERVAL '8 days', NULL),
('data scientist', 'new york', CURRENT_TIMESTAMP, NULL),
('golang', 'berlin', CURRENT_TIMESTAMP, CURRENT_TIMESTAMP - INTERVAL '30 minutes'),
('retry', 'berlin', CURRENT_TIMESTAMP, NULL);

INSERT INTO offers (id, title, company, location, posted_at, description, source, url) VALUES
('offer_001', 'Senior Python Developer', 'TechCorp Inc', 'San Francisco, CA', CURRENT_TIMESTAMP - INTERVAL '8 days', '', 'LinkedIn', ''),
('existing_offer', 'Junior Golang Dweeb', 'Späti GmbH', 'Berlin', CURRENT_TIMESTAMP, '', 'LinkedIn', 'https://www.linkedin.com/jobs/view/existing_offer'),
('existing_offer2', 'Senior Golang Dweeb', 'Späti GmbH', 'Berlin', CURRENT_TIMESTAMP, 'some nifty description', 'Stepstone', 'https://www.stepstone.de/senior_golang_dweeb');

INSERT INTO query_offers (query_id, offer_id) VALUES
(1, 'offer_001'),
(3, 'existing_offer'),
(3, 'existing_offer2'),
(1, 'existing_offer');
