#include <stdint.h>
#include "cpp-btree-1.0.1/btree_map.h"

//compact integer whose alignment=1
template<int byte_count>
struct bits_n {
	uint8_t b[byte_count];
	static bits_n from_uint64(uint64_t uint64) {
		bits_n v;
		for(int i=0; i<byte_count; i++) {
			v.b[i] = uint8_t(uint64>>(i*8));
		}
		return v;
	}
	static bits_n from_int64(int64_t int64) {
		return from_uint64(uint64_t(int64));
	}
	static bits_n from_uint32(uint32_t uint32) {
		return from_uint64(uint64_t(uint32));
	}
	uint64_t to_uint64() const {
		uint64_t v = 0;
		for(int i=byte_count; i>=0; i--) {
			v = (v<<8) | uint64_t(b[i]);
		}
		return v;
	}
	int64_t to_int64() const {
		return int64_t(to_uint64());
	}
	uint32_t to_uint32() const {
		return uint32_t(to_uint64());
	}
	bool operator<(const bits_n& other) const {
		return this->to_uint64() < other.to_uint64();
	}
};

typedef bits_n<3> bits24;
typedef bits_n<4> bits32;
typedef bits_n<5> bits40;
typedef bits_n<6> bits48;
typedef bits_n<7> bits56;
typedef bits_n<8> bits64;

// Bundle 'slot_count' basic_map together to construct a bigmap
// The conceptual key for bigmap is concat(idx, key)
template<int slot_count, typename key_type, typename value_type>
class bigmap {
public:
	typedef btree::btree_map<key_type, value_type> basic_map;
private:
	basic_map* _map_arr[slot_count];
	// make sure a slot do have a basic_map in it
	void create_if_null(int idx) {
		if(_map_arr[idx] == nullptr) {
			_map_arr[idx] = new basic_map;
		}
	}
public:
	bigmap() {
		for(int i = 0; i < slot_count; i++) {
			_map_arr[i] = nullptr;
		}
	}
	~bigmap() {
		for(int i = 0; i < slot_count; i++) {
			if(_map_arr[i] == nullptr) continue;
			delete _map_arr[i];
			_map_arr[i] = nullptr;
		}
	}
	bigmap(const bigmap& other) = delete;
	bigmap& operator=(const bigmap& other) = delete;
	bigmap(bigmap&& other) = delete;
	bigmap& operator=(bigmap&& other) = delete;

	// insert (k,v) at the basic_map in the 'idx'-th slot
	void insert(uint64_t idx, key_type k, value_type v) {
		idx %= slot_count;
		create_if_null(idx);
		_map_arr[idx]->insert(std::make_pair(k, v));
	}
	// erase (k,v) at the basic_map in the 'idx'-th slot
	void erase(uint64_t idx, key_type k) {
		idx %= slot_count;
		if(_map_arr[idx] == nullptr) return;
		_map_arr[idx]->erase(k);
	}
	// seek to a postion no less than the 'k' at the basic_map in the 'idx'-th slot
	// *ok indicates whether the returned iterator is valid
	typename basic_map::iterator seek(uint64_t idx, key_type k, bool* ok) {
		idx %= slot_count;
		typename basic_map::iterator it;
		*ok = false;
		if(_map_arr[idx] == nullptr) {
			return it;
		}
		it = _map_arr[idx]->lower_bound(k);
		if(it == _map_arr[idx]->end()) {
			return it;
		}
		*ok = true;
		return it;
	}
	// return k's corresponding value at the basic_map
	// *ok indicates whether the value can be found
	value_type get(uint64_t idx, key_type k, bool* ok) {
		idx %= slot_count;
		*ok = false;
		if(_map_arr[idx] == nullptr) {
			return value_type{};
		}
		auto it = _map_arr[idx]->find(k);
		if(it == _map_arr[idx]->end()) {
			return value_type{};
		}
		*ok = true;
		return it->second;
	}
	// return the sum of the sizes of all the basic_maps
	size_t size() {
		size_t total = 0;
		for(int i = 0; i < slot_count; i++) {
			if(_map_arr[i] == nullptr) continue;
			total += _map_arr[i]->size();
		}
		return total;
	}

	// bigmap's iterator can run accross the boundaries of slots
	class iterator {
		bigmap* _map; 
		int _curr_idx;
		int _end_idx; // _curr_idx <= _end_idx
		key_type _end_key; // _iter.key() can not equal _end_key
		typename basic_map::iterator _iter; // an iterator to _map._map_arr[_curr_idx]
		bool _valid;
	public:
		friend class bigmap;
		bool valid() {
			return _valid;
		}
		int curr_idx() {
			return _curr_idx();
		}
		key_type key() {
			return _iter->first;
		}
		value_type value() {
			return _iter->second;
		}
		// when this iterator points at the end of a slot, move it to the next valid position
		void handle_slot_crossing() {
			if(_iter != _map->_map_arr[_curr_idx]->end()) {
				return; // no need for slot crossing
			}
			_valid = false;
			for(_curr_idx++; !_valid && _curr_idx <= _end_idx; _curr_idx++) {
				if(_map->_map_arr[_curr_idx] != nullptr) continue; //skip null slot
				_iter = _map->_map_arr[_curr_idx]->begin();
				_valid = _iter != _map->_map_arr[_curr_idx]->end();
			}
		}
		void next() {
			if(!_valid) return;
			_iter++;
			handle_slot_crossing();
			_valid = _valid && _curr_idx <= _end_idx && _iter.key() < _end_key; 
		}
	};

	// Return a forward iterator. It starts at (start_idx,start_key) and ends 
	// at (end_idx, end_key), which is not included.
	iterator get_iterator(int start_idx, key_type start_key, int end_idx, key_type end_key) {
		class iterator iter;
		iter._valid = false;
		iter._end_idx = end_idx;
		iter._end_key = end_key;
		iter._map = this;
		if(_map_arr[iter._curr_idx] != nullptr) {
			iter._iter = _map_arr[iter._curr_idx]->lower_bound(start_key);
		} else {
			for(iter._curr_idx = start_idx; iter._curr_idx <= end_idx; iter._curr_idx++) {
				if(_map_arr[iter._curr_idx] != nullptr) break;
			}
			iter._iter = _map_arr[iter._curr_idx]->begin();
		}
		iter.handle_slot_crossing();
		return iter;
	}
};

