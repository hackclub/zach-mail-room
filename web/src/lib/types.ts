// Mirrors the Go JSON types in internal/swag and internal/store.

export type Item = {
	id: number;
	sku: string;
	name: string;
	description: string;
	image_url: string;
	visible: boolean;
	sort_order: number;
	max_per_request: number;
	max_per_user: number | null;
};

export type Settings = {
	requests_open: boolean;
	max_items_per_request: number;
	request_cooldown_days: number;
	international_shipping_fee_cents: number;
};

export type Catalog = {
	items: Item[];
	settings: Settings & { hcb_payment_url: string };
};

export type Address = {
	first_name: string;
	last_name: string;
	line_1: string;
	line_2: string;
	city: string;
	state: string;
	postal_code: string;
	country: string;
	phone_number: string;
};

export type Line = { item_id: number; quantity: number };

export type RequestStatus = 'awaiting_payment' | 'pending' | 'dispatched' | 'rejected' | 'cancelled';

export type SwagRequest = {
	id: number;
	user_id: number;
	user_email?: string;
	status: RequestStatus;
	address: Address;
	shipping_fee_cents: number;
	paid_at: string | null;
	note: string;
	admin_note: string;
	theseus_order_id: string;
	tracking_number: string;
	carrier: string;
	created_at: string;
	lines: { item_id: number; sku: string; name: string; quantity: number }[];
	payment_url?: string;
};

export type SubmissionStatus = 'submitted' | 'approved' | 'printing' | 'stocked' | 'rejected';

export type Submission = {
	id: number;
	user_email?: string;
	program: string;
	title: string;
	description: string;
	kind: string;
	file_url: string;
	quantity: number;
	status: SubmissionStatus;
	admin_note: string;
	sku: string;
	created_at: string;
};

export type User = { id: number; email: string; name: string; slack_id: string };
export type Me = { user: User | null; roles: { author: boolean; admin: boolean } };

export type WarehouseSKU = {
	sku: string;
	name: string;
	category: string;
	in_stock: number;
	inbound: number | null;
	unit_cost: string;
};
